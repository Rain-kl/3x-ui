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
	classMinor uint16
	ipHandles  map[string]string
}

type inboundState struct {
	rule             InboundRule
	applied          bool
	classMinor       uint16
	clients          map[string]*clientState
	nextFilterHandle int
}

func (s *inboundState) allocFilterHandle(inboundID int) string {
	node := ((inboundID & 0x7) << 8) | (s.nextFilterHandle & 0xff)
	s.nextFilterHandle++
	return fmt.Sprintf("800::%x", node)
}

// Reconciler synchronizes inbound and client rate limiting state with Linux TC.
type Reconciler struct {
	mu             sync.Mutex
	engine         *Engine
	inbounds       map[int]*inboundState
	debounceDelay  time.Duration
	debounceTimer  *time.Timer
	pending        map[int]map[string][]string
	usedMinors     map[uint16]bool
	nextMinor      uint16
	emailToInbound map[string]int
}

// NewReconciler creates a Reconciler bound to the specified Engine.
func NewReconciler(engine *Engine) *Reconciler {
	return &Reconciler{
		engine:         engine,
		inbounds:       make(map[int]*inboundState),
		debounceDelay:  time.Second,
		pending:        make(map[int]map[string][]string),
		usedMinors:     map[uint16]bool{0: true, 1: true, 0x9999: true},
		nextMinor:      0x0f,
		emailToInbound: make(map[string]int),
	}
}

func (r *Reconciler) allocMinorLocked() (uint16, error) {
	for count := 0; count < 0xffff; count++ {
		r.nextMinor++
		if r.nextMinor <= 1 || r.nextMinor == 0x9999 {
			continue
		}
		if !r.usedMinors[r.nextMinor] {
			r.usedMinors[r.nextMinor] = true
			return r.nextMinor, nil
		}
	}
	return 0, errors.New("exhausted 16-bit TC class IDs")
}

func (r *Reconciler) freeMinorLocked(minor uint16) {
	delete(r.usedMinors, minor)
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
			nextFilterHandle: 1,
		}
		r.inbounds[rule.InboundID] = state
	}

	if state.classMinor == 0 {
		minor, err := r.allocMinorLocked()
		if err != nil {
			return err
		}
		state.classMinor = minor
	}

	iface := r.engine.Interface()
	inboundClassID := fmt.Sprintf("1:%x", state.classMinor)

	rateStr := DefaultBandwidth
	if rule.InboundDownLimit > 0 {
		rateStr = fmt.Sprintf("%dmbit", rule.InboundDownLimit)
	}

	if err := r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", DefaultRootClassID, "classid", inboundClassID, "htb", "rate", rateStr, "ceil", rateStr, "burst", "64k", "cburst", "64k"); err != nil {
		return err
	}

	if !state.applied || state.rule.Port != rule.Port {
		inboundFilterHandle := fmt.Sprintf("0x%x", rule.InboundID)
		if state.applied && state.rule.Port != rule.Port {
			_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "10", "handle", inboundFilterHandle, "u32")
		}
		if err := r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "10", "handle", inboundFilterHandle, "u32", "match", "ip", "sport", strconv.Itoa(rule.Port), "0xffff", "flowid", inboundClassID); err != nil {
			return err
		}
	}

	// Update existing client filters if port changed on active inbound.
	if exists && state.applied && state.rule.Port != rule.Port {
		for _, client := range state.clients {
			subClassID := fmt.Sprintf("1:%x", client.classMinor)
			for ip, h := range client.ipHandles {
				if formattedIP, ok := formatIPv4Mask(ip); ok {
					_ = r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32", "match", "ip", "sport", strconv.Itoa(rule.Port), "0xffff", "match", "ip", "dst", formattedIP, "flowid", subClassID)
				}
			}
		}
	}

	// Update existing client classes if per-client bandwidth limit changed.
	if state.applied && state.rule.ClientDownLimit != rule.ClientDownLimit {
		if rule.ClientDownLimit > 0 {
			for _, client := range state.clients {
				subClassID := fmt.Sprintf("1:%x", client.classMinor)
				rateStr, ceilStr := clientRateParams(rule.InboundDownLimit, rule.ClientDownLimit)
				_ = r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", inboundClassID, "classid", subClassID, "htb", "rate", rateStr, "ceil", ceilStr, "burst", "32k", "cburst", "32k")
			}
		} else {
			for _, client := range state.clients {
				subClassID := fmt.Sprintf("1:%x", client.classMinor)
				for _, h := range client.ipHandles {
					_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32")
				}
				_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
				r.freeMinorLocked(client.classMinor)
			}
			state.clients = make(map[string]*clientState)
		}
	}

	state.rule = rule
	state.applied = true

	if len(rule.Clients) > 0 {
		for email, ibID := range r.emailToInbound {
			if ibID == rule.InboundID {
				delete(r.emailToInbound, email)
			}
		}
		for _, email := range rule.Clients {
			if email != "" {
				r.emailToInbound[email] = rule.InboundID
			}
		}
	}

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

	for email, ibID := range r.emailToInbound {
		if ibID == inboundID {
			delete(r.emailToInbound, email)
		}
	}

	iface := r.engine.Interface()
	inboundClassID := fmt.Sprintf("1:%x", state.classMinor)

	for _, client := range state.clients {
		subClassID := fmt.Sprintf("1:%x", client.classMinor)
		for _, h := range client.ipHandles {
			_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32")
		}
		_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
		r.freeMinorLocked(client.classMinor)
	}

	inboundFilterHandle := fmt.Sprintf("0x%x", inboundID)
	_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "10", "handle", inboundFilterHandle, "u32")
	_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", inboundClassID)

	r.freeMinorLocked(state.classMinor)
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
	inboundClassID := fmt.Sprintf("1:%x", state.classMinor)
	rateStr, ceilStr := clientRateParams(state.rule.InboundDownLimit, state.rule.ClientDownLimit)

	client, clientExists := state.clients[email]

	newIPs := make(map[string]string)
	for _, raw := range ips {
		if canonicalIP, formattedCIDR, ok := parseCanonicalIPv4(raw); ok {
			newIPs[canonicalIP] = formattedCIDR
		}
	}

	// Client disconnected or has no active IPs: remove client filters and leaf class.
	if len(newIPs) == 0 {
		if clientExists {
			subClassID := fmt.Sprintf("1:%x", client.classMinor)
			for _, h := range client.ipHandles {
				_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32")
			}
			_ = r.engine.Execute(ctx, "tc", "class", "del", "dev", iface, "classid", subClassID)
			r.freeMinorLocked(client.classMinor)
			delete(state.clients, email)
		}
		return nil
	}

	if !clientExists {
		minor, err := r.allocMinorLocked()
		if err != nil {
			return err
		}
		client = &clientState{
			classMinor: minor,
			ipHandles:  make(map[string]string),
		}
		state.clients[email] = client
	}

	subClassID := fmt.Sprintf("1:%x", client.classMinor)

	if err := r.engine.Execute(ctx, "tc", "class", "replace", "dev", iface, "parent", inboundClassID, "classid", subClassID, "htb", "rate", rateStr, "ceil", ceilStr, "burst", "32k", "cburst", "32k"); err != nil {
		return err
	}
	_ = r.engine.Execute(ctx, "tc", "qdisc", "replace", "dev", iface, "parent", subClassID, "fq_codel")

	for oldIP, h := range client.ipHandles {
		if _, stillPresent := newIPs[oldIP]; !stillPresent {
			_ = r.engine.Execute(ctx, "tc", "filter", "del", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32")
			delete(client.ipHandles, oldIP)
		}
	}

	for newIP, formattedCIDR := range newIPs {
		if _, alreadyPresent := client.ipHandles[newIP]; !alreadyPresent {
			h := state.allocFilterHandle(inboundID)
			if err := r.engine.Execute(ctx, "tc", "filter", "replace", "dev", iface, "protocol", "ip", "parent", "1:0", "prio", "5", "handle", h, "u32", "match", "ip", "sport", strconv.Itoa(state.rule.Port), "0xffff", "match", "ip", "dst", formattedCIDR, "flowid", subClassID); err != nil {
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

// RegisterClientInbound maps a client email to an inbound ID.
func (r *Reconciler) RegisterClientInbound(email string, inboundID int) {
	if email == "" || inboundID <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.emailToInbound == nil {
		r.emailToInbound = make(map[string]int)
	}
	r.emailToInbound[email] = inboundID
}

// SyncAllObserved reconciles observed active client IPs across all managed inbounds.
func (r *Reconciler) SyncAllObserved(observed map[string]map[string]int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	observedIPs := make(map[string][]string, len(observed))
	for email, ipMap := range observed {
		ips := make([]string, 0, len(ipMap))
		for ip := range ipMap {
			ips = append(ips, ip)
		}
		observedIPs[email] = ips
	}

	for inboundID, state := range r.inbounds {
		if !state.applied || state.rule.ClientDownLimit <= 0 {
			continue
		}
		syncedEmails := make(map[string]bool)
		for email, ips := range observedIPs {
			belongs := false
			if r.emailToInbound != nil && r.emailToInbound[email] != 0 {
				belongs = r.emailToInbound[email] == inboundID
			} else if _, exists := state.clients[email]; exists {
				belongs = true
			}
			if belongs {
				syncedEmails[email] = true
				_ = r.syncClientIPsLocked(context.Background(), inboundID, email, ips)
			}
		}
		for email := range state.clients {
			if !syncedEmails[email] {
				_ = r.syncClientIPsLocked(context.Background(), inboundID, email, nil)
			}
		}
	}
}

func parseCanonicalIPv4(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	var parsed net.IP
	if ip, _, err := net.ParseCIDR(trimmed); err == nil {
		parsed = ip
	} else {
		parsed = net.ParseIP(trimmed)
	}
	if parsed == nil {
		return "", "", false
	}
	v4 := parsed.To4()
	if v4 == nil {
		return "", "", false
	}
	canonical := v4.String()
	return canonical, canonical + "/32", true
}

func formatIPv4Mask(raw string) (string, bool) {
	_, formattedCIDR, ok := parseCanonicalIPv4(raw)
	return formattedCIDR, ok
}

// clientRateParams guarantees minimum rate while enforcing ceiling to honor parent inbound limits.
func clientRateParams(inboundLimit, clientLimit int) (string, string) {
	ceilStr := fmt.Sprintf("%dmbit", clientLimit)
	if inboundLimit <= 0 || clientLimit <= 1 {
		return ceilStr, ceilStr
	}
	return "1mbit", ceilStr
}
