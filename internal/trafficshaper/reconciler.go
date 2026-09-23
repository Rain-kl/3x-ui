package trafficshaper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	defaultReconcilerLock sync.RWMutex
	defaultReconciler     *Reconciler
)

// GetReconciler returns the process-wide trafficshaper reconciler singleton.
func GetReconciler() *Reconciler {
	defaultReconcilerLock.RLock()
	defer defaultReconcilerLock.RUnlock()
	return defaultReconciler
}

// SetReconciler registers the process-wide trafficshaper reconciler singleton.
func SetReconciler(r *Reconciler) {
	defaultReconcilerLock.Lock()
	defer defaultReconcilerLock.Unlock()
	defaultReconciler = r
}

type clientState struct {
	classIdx  int
	ipHandles map[string]int
}

type inboundState struct {
	rule             InboundRule
	applied          bool
	clients          map[string]*clientState
	nextClassIdx     int
	freeClassIdx     []int
	nextFilterHandle int
}

func (s *inboundState) allocClientClassIdx() int {
	if len(s.freeClassIdx) > 0 {
		idx := s.freeClassIdx[len(s.freeClassIdx)-1]
		s.freeClassIdx = s.freeClassIdx[:len(s.freeClassIdx)-1]
		return idx
	}
	idx := s.nextClassIdx
	s.nextClassIdx++
	return idx
}

func (s *inboundState) allocFilterHandle(inboundID int) int {
	h := (inboundID * 10000) + s.nextFilterHandle
	s.nextFilterHandle++
	return h
}

// Reconciler synchronizes inbound and client rate limiting state with Linux TC.
type Reconciler struct {
	mu            sync.Mutex
	engine        *Engine
	inbounds      map[int]*inboundState
	debounceDelay time.Duration
	debounceTimer *time.Timer
	pending       map[int]map[string][]string
}

// NewReconciler creates a Reconciler bound to the specified Engine.
func NewReconciler(engine *Engine) *Reconciler {
	return &Reconciler{
		engine:        engine,
		inbounds:      make(map[int]*inboundState),
		debounceDelay: time.Second,
		pending:       make(map[int]map[string][]string),
	}
}

// SetDebounceDelay configures the batch aggregation window for client IP updates.
func (r *Reconciler) SetDebounceDelay(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.debounceDelay = d
}

// Stop cancels any pending debounce timers.
func (r *Reconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.debounceTimer != nil {
		r.debounceTimer.Stop()
		r.debounceTimer = nil
	}
}

// UpdateInbound updates inbound rate limits using context.Background.
func (r *Reconciler) UpdateInbound(rule InboundRule) error {
	return r.ApplyInbound(context.Background(), rule)
}

// RemoveInbound removes inbound rate limiting rules by inbound ID.
func (r *Reconciler) RemoveInbound(inboundID int) error {
	return r.RemoveInboundWithContext(context.Background(), inboundID)
}

// RemoveInboundWithContext removes all TC classes and filters for the inbound.
func (r *Reconciler) RemoveInboundWithContext(ctx context.Context, inboundID int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if inboundID <= 0 {
		return fmt.Errorf("invalid inbound ID: %d", inboundID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.removeInboundLocked(ctx, inboundID)
}

// ApplyInbound incrementally updates TC classes and port filters for an inbound.
func (r *Reconciler) ApplyInbound(ctx context.Context, rule InboundRule) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if rule.InboundID <= 0 || rule.Port <= 0 {
		return fmt.Errorf("invalid inbound rule: ID=%d, Port=%d", rule.InboundID, rule.Port)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.applyInboundLocked(ctx, rule)
}

func (r *Reconciler) applyInboundLocked(ctx context.Context, rule InboundRule) error {
	if rule.InboundDownLimit <= 0 && rule.ClientDownLimit <= 0 {
		return r.removeInboundLocked(ctx, rule.InboundID)
	}

	state, exists := r.inbounds[rule.InboundID]
	if !exists {
		state = &inboundState{
			rule:             rule,
			clients:          make(map[string]*clientState),
			nextClassIdx:     1,
			nextFilterHandle: 1,
		}
		r.inbounds[rule.InboundID] = state
	}

	iface := r.engine.Interface()
	inboundClassID := formatInboundClassID(rule.InboundID)

	rateStr := DefaultBandwidth
	if rule.InboundDownLimit > 0 {
		rateStr = fmt.Sprintf("%dmbit", rule.InboundDownLimit)
	}

	if err := r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", DefaultRootClassID, "classid", inboundClassID, "htb", "rate", rateStr, "ceil", rateStr); err != nil {
		return err
	}

	inboundFilterHandle := strconv.Itoa(rule.InboundID)
	if err := r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "10", "handle", inboundFilterHandle, "u32", "match", "ip", "sport", strconv.Itoa(rule.Port), "0xffff", "flowid", inboundClassID); err != nil {
		return err
	}

	// Update existing client filters if port changed on active inbound.
	if exists && state.applied && state.rule.Port != rule.Port {
		for _, client := range state.clients {
			subClassID := formatClientClassID(rule.InboundID, client.classIdx)
			for ip, h := range client.ipHandles {
				if formattedIP, ok := formatIPv4Mask(ip); ok {
					_ = r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32", "match", "ip", "sport", strconv.Itoa(rule.Port), "0xffff", "match", "ip", "dst", formattedIP, "flowid", subClassID)
				}
			}
		}
	}

	// Update existing client classes if per-client bandwidth limit changed.
	if state.applied && state.rule.ClientDownLimit != rule.ClientDownLimit {
		if rule.ClientDownLimit > 0 {
			clientRateStr := fmt.Sprintf("%dmbit", rule.ClientDownLimit)
			for _, client := range state.clients {
				subClassID := formatClientClassID(rule.InboundID, client.classIdx)
				_ = r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", inboundClassID, "classid", subClassID, "htb", "rate", clientRateStr, "ceil", clientRateStr)
			}
		} else {
			for _, client := range state.clients {
				subClassID := formatClientClassID(rule.InboundID, client.classIdx)
				for _, h := range client.ipHandles {
					_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32")
				}
				_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
			}
			state.clients = make(map[string]*clientState)
		}
	}

	state.rule = rule
	state.applied = true

	if len(rule.ActiveIPs) > 0 {
		_ = r.syncClientIPsLocked(ctx, rule.InboundID, "default", rule.ActiveIPs)
	}

	return nil
}

func (r *Reconciler) removeInboundLocked(ctx context.Context, inboundID int) error {
	state, exists := r.inbounds[inboundID]
	if !exists {
		return nil
	}

	iface := r.engine.Interface()
	inboundClassID := formatInboundClassID(inboundID)

	for _, client := range state.clients {
		subClassID := formatClientClassID(inboundID, client.classIdx)
		for _, h := range client.ipHandles {
			_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32")
		}
		_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
	}

	inboundFilterHandle := strconv.Itoa(inboundID)
	_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "10", "handle", inboundFilterHandle, "u32")
	_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", inboundClassID)

	delete(r.inbounds, inboundID)
	return nil
}

// SyncClientIPs immediately updates TC rules for a client on the specified inbound.
func (r *Reconciler) SyncClientIPs(ctx context.Context, inboundID int, email string, ips []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.syncClientIPsLocked(ctx, inboundID, email, ips)
}

func (r *Reconciler) syncClientIPsLocked(ctx context.Context, inboundID int, email string, ips []string) error {
	state, exists := r.inbounds[inboundID]
	if !exists || !state.applied {
		return fmt.Errorf("inbound %d not found or not applied", inboundID)
	}
	if state.rule.ClientDownLimit <= 0 {
		return nil
	}

	iface := r.engine.Interface()
	inboundClassID := formatInboundClassID(inboundID)
	clientRateStr := fmt.Sprintf("%dmbit", state.rule.ClientDownLimit)

	client, clientExists := state.clients[email]

	newIPs := make(map[string]string)
	for _, ip := range ips {
		if formatted, ok := formatIPv4Mask(ip); ok {
			newIPs[ip] = formatted
		}
	}

	// Client disconnected or has no active IPs: remove client filters and leaf class.
	if len(newIPs) == 0 {
		if clientExists {
			subClassID := formatClientClassID(inboundID, client.classIdx)
			for _, h := range client.ipHandles {
				_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32")
			}
			_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
			state.freeClassIdx = append(state.freeClassIdx, client.classIdx)
			delete(state.clients, email)
		}
		return nil
	}

	if !clientExists {
		client = &clientState{
			classIdx:  state.allocClientClassIdx(),
			ipHandles: make(map[string]int),
		}
		state.clients[email] = client
	}

	subClassID := formatClientClassID(inboundID, client.classIdx)

	if err := r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", inboundClassID, "classid", subClassID, "htb", "rate", clientRateStr, "ceil", clientRateStr); err != nil {
		return err
	}
	_ = r.engine.Execute(ctx, "tc", "qdisc", "replace", "dev", iface, "parent", subClassID, "fq_codel")

	for oldIP, h := range client.ipHandles {
		if _, stillPresent := newIPs[oldIP]; !stillPresent {
			_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32")
			delete(client.ipHandles, oldIP)
		}
	}

	for newIP, formattedIP := range newIPs {
		if _, alreadyPresent := client.ipHandles[newIP]; !alreadyPresent {
			h := state.allocFilterHandle(inboundID)
			if err := r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", strconv.Itoa(h), "u32", "match", "ip", "sport", strconv.Itoa(state.rule.Port), "0xffff", "match", "ip", "dst", formattedIP, "flowid", subClassID); err != nil {
				return err
			}
			client.ipHandles[newIP] = h
		}
	}

	return nil
}

// QueueClientIPs buffers a client IP update for asynchronous debounced reconciliation.
func (r *Reconciler) QueueClientIPs(inboundID int, email string, ips []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.pending[inboundID]; !ok {
		r.pending[inboundID] = make(map[string][]string)
	}
	copied := make([]string, len(ips))
	copy(copied, ips)
	r.pending[inboundID][email] = copied

	if r.debounceTimer != nil {
		r.debounceTimer.Stop()
	}
	delay := r.debounceDelay
	if delay <= 0 {
		delay = time.Second
	}
	r.debounceTimer = time.AfterFunc(delay, func() {
		_ = r.Flush(context.Background())
	})
}

// Flush applies all buffered debounced IP updates, accumulating any errors.
func (r *Reconciler) Flush(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.debounceTimer != nil {
		r.debounceTimer.Stop()
		r.debounceTimer = nil
	}
	if len(r.pending) == 0 {
		return nil
	}

	toProcess := r.pending
	r.pending = make(map[int]map[string][]string)

	var errs []error
	for inboundID, clients := range toProcess {
		for email, ips := range clients {
			if err := r.syncClientIPsLocked(ctx, inboundID, email, ips); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func formatInboundClassID(inboundID int) string {
	return fmt.Sprintf("1:%d0", inboundID)
}

func formatClientClassID(inboundID int, clientIdx int) string {
	return fmt.Sprintf("1:%d%04d", inboundID, clientIdx)
}

func formatIPv4Mask(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if ip, _, err := net.ParseCIDR(raw); err == nil {
		if v4 := ip.To4(); v4 != nil {
			return raw, true
		}
		return "", false
	}
	parsed := net.ParseIP(raw)
	if parsed == nil {
		return "", false
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String() + "/32", true
	}
	return "", false
}
