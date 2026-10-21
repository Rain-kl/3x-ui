package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/routingsync"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

const (
	routingSyncDesiredKey  = "routingSyncDesired"
	routingSyncReceivedKey = "routingSyncReceived"
	routingSyncSourceKey   = "panelSyncSource"
)

var xrayTemplateMu sync.Mutex

type routingReceipt struct {
	Source    string                 `json:"source"`
	Revision  int64                  `json:"revision"`
	Hash      string                 `json:"hash"`
	Applied   bool                   `json:"applied"`
	Balancing *routingsync.Balancing `json:"balancing,omitempty"`
}

type RoutingSyncService struct{}

func readRoutingValue(tx *gorm.DB, key string, target any) (bool, error) {
	var rows []model.Setting
	if err := tx.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	if len(rows) == 0 {
		return false, nil
	}
	if err := json.Unmarshal([]byte(rows[0].Value), target); err != nil {
		return false, fmt.Errorf("decode %s: %w", key, err)
	}
	return true, nil
}

func saveRoutingValue(tx *gorm.DB, key, value string) error {
	if err := tx.Where("key = ?", key).Assign(model.Setting{Value: value}).FirstOrCreate(&model.Setting{Key: key}).Error; err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}

func saveRoutingJSON(tx *gorm.DB, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return saveRoutingValue(tx, key, string(data))
}

func selectedSyncRules(template string) ([]map[string]any, error) {
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return nil, err
	}
	selected := make([]map[string]any, 0)
	for i, rule := range rules {
		flag, exists := rule["sync"]
		if exists {
			if _, ok := flag.(bool); !ok {
				return nil, fmt.Errorf("routing rule %d: sync must be a boolean", i+1)
			}
		}
		if flag == true && rule[routingSyncSourceKey] == nil {
			selected = append(selected, rule)
		}
	}
	return selected, validateSyncRuleSet(selected)
}

func validateSyncRuleSet(rules []map[string]any) error {
	if len(rules) > 10000 {
		return fmt.Errorf("routing synchronization exceeds 10000 rules")
	}
	for i, rule := range rules {
		if rule["localPre"] == true {
			return fmt.Errorf("routing sync rule %d: local pre-routing rules cannot be synchronized", i+1)
		}
		if rule == nil {
			return fmt.Errorf("routing sync rule %d is null", i+1)
		}
		if enabled, ok := rule["enabled"]; ok {
			if _, ok := enabled.(bool); !ok {
				return fmt.Errorf("routing sync rule %d: enabled must be a boolean", i+1)
			}
		}
		if isApiRule(rule) || rule["outboundTag"] == "api" {
			return fmt.Errorf("routing sync rule %d: api control rules cannot be synchronized", i+1)
		}
		for _, tag := range readInboundTags(rule["inboundTag"]) {
			if tag == "api" || strings.HasPrefix(tag, "_bl_") {
				return fmt.Errorf("routing sync rule %d: internal inbound %q cannot be synchronized", i+1, tag)
			}
		}
	}
	return nil
}

func persistRoutingDesired(tx *gorm.DB, rules []map[string]any, balancing *routingsync.Balancing, source string) error {
	var receipt routingReceipt
	if _, err := readRoutingValue(tx, routingSyncReceivedKey, &receipt); err != nil {
		return err
	}
	if receipt.Source != "" {
		return nil
	}
	var state routingsync.Control
	found, err := readRoutingValue(tx, routingSyncDesiredKey, &state)
	if err != nil || (!found && len(rules) == 0 && len(balancing.Balancers) == 0) {
		return err
	}
	old := routingSnapshotContent(state)
	next := routingSnapshotContent(routingsync.Control{Rules: rules, Balancing: balancing})
	if found && string(old) == string(next) {
		return nil
	}
	if !found {
		state.Source = source
	}
	if state.Source == "" || state.Revision == int64(^uint64(0)>>1) {
		return fmt.Errorf("invalid routing sync identity or exhausted revision")
	}
	state.Revision++
	state.Rules = rules
	state.Balancing = balancing
	return saveRoutingJSON(tx, routingSyncDesiredKey, state)
}

// keepManagedRouting preserves authoritative copies across local or legacy remote saves.
func keepManagedRouting(oldTemplate, template string) (string, error) {
	var receipt routingReceipt
	if _, err := readRoutingValue(database.GetDB(), routingSyncReceivedKey, &receipt); err != nil {
		return "", err
	}
	var err error
	template, err = installBalancing(template, receipt.Balancing, receipt.Source)
	if err != nil {
		return "", err
	}
	oldRules, err := GetRoutingRulesFromTemplate(oldTemplate)
	if err != nil {
		return "", err
	}
	rules, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return "", err
	}
	managed := make([]map[string]any, 0)
	for _, rule := range oldRules {
		if source, _ := rule[routingSyncSourceKey].(string); source != "" {
			managed = append(managed, rule)
		}
	}
	if len(managed) > 0 {
		var settings struct {
			Routing struct {
				Script string `json:"script"`
			} `json:"routing"`
		}
		if err := json.Unmarshal([]byte(template), &settings); err != nil {
			return "", fmt.Errorf("read child routing script: %w", err)
		}
		if settings.Routing.Script != "" {
			return "", fmt.Errorf("routing script would override parent-managed rules; remove the script or withdraw synchronized rules first")
		}
	}
	local := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		if _, exists := rule[routingSyncSourceKey]; !exists {
			local = append(local, rule)
		}
	}
	if len(managed) == 0 && len(local) == len(rules) {
		return template, nil
	}
	updated, _, err := ReplaceRoutingRulesInTemplate(template, append(managed, local...))
	return updated, err
}

func validateSyncTargets(rules []map[string]any, template string, balancing *routingsync.Balancing) error {
	if len(rules) == 0 && balancing == nil {
		return nil
	}
	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		return fmt.Errorf("build node configuration for routing references: %w", err)
	}
	inbounds, outbounds, balancers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, inbound := range cfg.InboundConfigs {
		inbounds[inbound.Tag] = true
	}
	var obs []struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(cfg.OutboundConfigs, &obs); err != nil {
		return fmt.Errorf("decode node outbounds: %w", err)
	}
	for _, outbound := range obs {
		outbounds[outbound.Tag] = true
	}
	oldTemplate, err := (&SettingService{}).GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	oldTags, err := GetOutboundTagsFromTemplate(oldTemplate)
	if err != nil {
		return err
	}
	for _, tag := range oldTags {
		delete(outbounds, tag)
	}
	newTags, err := GetOutboundTagsFromTemplate(template)
	if err != nil {
		return err
	}
	for _, tag := range newTags {
		outbounds[tag] = true
		if strings.HasPrefix(tag, "_bl_") {
			inbounds[tag] = true
		}
	}
	if err := validateBalancingTargets(balancing, outbounds); err != nil {
		return err
	}
	var candidate struct {
		Routing json.RawMessage `json:"routing"`
	}
	if err := json.Unmarshal([]byte(template), &candidate); err != nil {
		return err
	}
	var routing struct {
		Script    string `json:"script"`
		Balancers []struct {
			Tag string `json:"tag"`
		} `json:"balancers"`
	}
	if err := json.Unmarshal(candidate.Routing, &routing); err != nil {
		return fmt.Errorf("decode node balancers: %w", err)
	}
	if routing.Script != "" {
		return fmt.Errorf("node routing script overrides field rules; remove the script before enabling routing synchronization")
	}
	for _, balancer := range routing.Balancers {
		balancers[balancer.Tag] = true
	}
	if balancing != nil {
		localRules, err := GetRoutingRulesFromTemplate(template)
		if err != nil {
			return err
		}
		for _, rule := range localRules {
			tag, _ := rule["balancerTag"].(string)
			if tag != "" && rule["enabled"] != false && !balancers[tag] {
				return fmt.Errorf("node routing rule references missing synchronized balancer %q", tag)
			}
		}
	}
	for i, rule := range rules {
		if rule["enabled"] == false || isApiRule(rule) {
			continue
		}
		outbound, _ := rule["outboundTag"].(string)
		balancer, _ := rule["balancerTag"].(string)
		if (outbound == "") == (balancer == "") {
			return fmt.Errorf("routing sync rule %d requires exactly one outbound or balancer", i+1)
		}
		if outbound != "" && !outbounds[outbound] {
			return fmt.Errorf("routing sync rule %d: node outbound %q is missing", i+1, outbound)
		}
		if balancer != "" && !balancers[balancer] {
			return fmt.Errorf("routing sync rule %d: node balancer %q is missing", i+1, balancer)
		}
		for _, tag := range readInboundTags(rule["inboundTag"]) {
			if !inbounds[tag] {
				return fmt.Errorf("routing sync rule %d: node inbound %q is missing", i+1, tag)
			}
		}
	}
	return nil
}

// Apply installs a parent snapshot and only acknowledges it after runtime reconciliation.
func (RoutingSyncService) Apply(state routingsync.Control) error {
	if state.Source == "" || len(state.Source) > 64 || state.Revision <= 0 || state.Rules == nil {
		return fmt.Errorf("routing sync requires source, positive revision and an explicit rules array")
	}
	if err := validateSyncRuleSet(state.Rules); err != nil {
		return err
	}
	xrayTemplateMu.Lock()
	defer xrayTemplateMu.Unlock()
	db := database.GetDB()
	var downstream int64
	if err := db.Model(&model.Node{}).Where("enable = ?", true).Count(&downstream).Error; err != nil {
		return fmt.Errorf("check routing sync node role: %w", err)
	}
	if downstream > 0 {
		return fmt.Errorf("routing sync requires a direct child without enabled downstream nodes")
	}
	var receipt routingReceipt
	if _, err := readRoutingValue(db, routingSyncReceivedKey, &receipt); err != nil {
		return err
	}
	if receipt.Source != "" && receipt.Source != state.Source {
		return fmt.Errorf("routing sync is already managed by parent %s", receipt.Source)
	}
	encoded := routingSnapshotContent(state)
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	if state.Revision < receipt.Revision {
		return nil
	}
	if state.Revision == receipt.Revision {
		if hash != receipt.Hash {
			return fmt.Errorf("routing sync revision %d has conflicting content", state.Revision)
		}
		if receipt.Applied {
			return nil
		}
	}
	template, err := (&SettingService{}).GetXrayConfigTemplate()
	if err != nil {
		return err
	}
	current, err := GetRoutingRulesFromTemplate(template)
	if err != nil {
		return err
	}
	rules := make([]map[string]any, 0, len(state.Rules)+len(current))
	for _, rule := range state.Rules {
		copyRule := make(map[string]any, len(rule)+1)
		for key, value := range rule {
			if key != "sync" && key != "localPre" && key != routingSyncSourceKey {
				copyRule[key] = value
			}
		}
		copyRule[routingSyncSourceKey] = state.Source
		rules = append(rules, copyRule)
	}
	for _, rule := range current {
		if _, managed := rule[routingSyncSourceKey]; !managed {
			rules = append(rules, rule)
		}
	}
	updated, _, err := ReplaceRoutingRulesInTemplate(template, rules)
	if err != nil {
		return err
	}
	balancing := state.Balancing
	if balancing == nil {
		balancing = receipt.Balancing
	}
	updated, err = installBalancing(updated, balancing, state.Source)
	if err != nil {
		return err
	}
	if err := validateSyncTargets(state.Rules, updated, balancing); err != nil {
		return err
	}
	if err := (&XraySettingService{}).CheckXrayConfig(updated); err != nil {
		return fmt.Errorf("validate synchronized routing: %w", err)
	}
	if state.Balancing != nil {
		receipt.Balancing = state.Balancing
	}
	receipt = routingReceipt{Source: state.Source, Revision: state.Revision, Hash: hash, Balancing: receipt.Balancing}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := saveRoutingValue(tx, "xrayTemplateConfig", updated); err != nil {
			return err
		}
		return saveRoutingJSON(tx, routingSyncReceivedKey, receipt)
	}); err != nil {
		return fmt.Errorf("persist synchronized routing: %w", err)
	}
	xraySvc := &XrayService{}
	if xraySvc.IsXrayRunning() {
		if err := xraySvc.RestartXray(false); err != nil {
			return fmt.Errorf("routing saved; applying synchronized routing failed: %w", err)
		}
	}
	receipt.Applied = true
	return saveRoutingJSON(db, routingSyncReceivedKey, receipt)
}

// SyncNodes sends the latest persisted snapshot; callers can retry after partial delivery.
func (RoutingSyncService) SyncNodes(ctx context.Context) error {
	var state routingsync.Control
	found, err := readRoutingValue(database.GetDB(), routingSyncDesiredKey, &state)
	if err != nil || !found {
		return err
	}
	nodes, err := (&NodeService{}).GetAll()
	if err != nil {
		return fmt.Errorf("list routing sync targets: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results := make([]error, len(nodes))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, node := range nodes {
		if !node.Enable {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			results[i] = fmt.Errorf("node %s (%d): %w", node.Name, node.Id, ctx.Err())
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rt := runtime.NewRemote(node, nil)
			if mgr := runtime.GetManager(); mgr != nil {
				var lookupErr error
				rt, lookupErr = mgr.RemoteFor(node)
				if lookupErr != nil {
					results[i] = fmt.Errorf("node %s (%d): %w", node.Name, node.Id, lookupErr)
					return
				}
			}
			nodeCtx, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			if err := rt.SyncRouting(nodeCtx, state); err != nil {
				results[i] = fmt.Errorf("node %s (%d): %w", node.Name, node.Id, err)
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(results...); err != nil {
		return fmt.Errorf("routing saved locally; node synchronization pending (automatic retry): %w", err)
	}
	return nil
}

// Legacy snapshots retain their original hash so existing pending receipts can be retried.
func routingSnapshotContent(state routingsync.Control) []byte {
	var value any = state.Rules
	if state.Balancing != nil {
		value = struct {
			Rules     []map[string]any       `json:"rules"`
			Balancing *routingsync.Balancing `json:"balancing"`
		}{state.Rules, state.Balancing}
	}
	data, _ := json.Marshal(value)
	return data
}
