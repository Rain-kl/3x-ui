package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestGenXrayInboundConfigInjectsNativeRateLimits(t *testing.T) {
	inbound := &model.Inbound{
		Protocol:         model.VLESS,
		Tag:              "vless-443",
		Settings:         `{"clients":[{"email":"default"},{"email":"override","downLimit":7}],"decryption":"none"}`,
		InboundDownLimit: 12,
		ClientDownLimit:  3,
	}
	config := inbound.GenXrayInboundConfig()
	var settings map[string]any
	if err := json.Unmarshal(config.Settings, &settings); err != nil {
		t.Fatalf("unmarshal generated inbound settings: %v", err)
	}
	clients := settings["clients"].([]any)
	for i, want := range []struct {
		email string
		limit float64
	}{{"default", 375000}, {"override", 875000}} {
		user := clients[i].(map[string]any)
		if user["email"] != xray.ScopedEmail("vless-443", want.email) || user["downlink_speed_limit"] != want.limit {
			t.Errorf("client %d = %v, want email %q with cap %v", i, user, want.email, want.limit)
		}
		if user["rate_limit_group"] != "inbound:vless-443" || user["downlink_group_speed_limit"] != float64(1500000) {
			t.Errorf("client %q has group %v / cap %v, want inbound:vless-443 / 1500000", want.email, user["rate_limit_group"], user["downlink_group_speed_limit"])
		}
	}
	if settings["decryption"] != "none" {
		t.Fatalf("core setting was not preserved: %v", settings)
	}
}

func TestGetXrayConfigAppliesNormalizedClientRateLimits(t *testing.T) {
	db := initTrafficTestDB(t)
	if err := (&SettingService{}).setString("xrayTemplateConfig", `{}`); err != nil {
		t.Fatalf("save xray template: %v", err)
	}
	inbound := &model.Inbound{
		UserId: 1, Enable: true, Protocol: model.VLESS, Tag: "normalized-in", Port: 444,
		Settings: `{"clients":[],"decryption":"none"}`, InboundDownLimit: 15, ClientDownLimit: 3,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	clients := []struct {
		recordLimit int
		linkLimit   int
	}{
		{recordLimit: 0, linkLimit: 0},
		{recordLimit: 7, linkLimit: 9},
	}
	for i, limits := range clients {
		record := &model.ClientRecord{Email: []string{"default@test", "override@test"}[i], UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Enable: true, DownLimit: limits.recordLimit}
		if err := db.Create(record).Error; err != nil {
			t.Fatalf("create client %d: %v", i, err)
		}
		link := &model.ClientInbound{ClientId: record.Id, InboundId: inbound.Id, DownLimit: limits.linkLimit}
		if err := db.Create(link).Error; err != nil {
			t.Fatalf("attach client %d: %v", i, err)
		}
	}
	config, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatalf("GetXrayConfig: %v", err)
	}
	if len(config.InboundConfigs) != 1 {
		t.Fatalf("inbound count = %d, want 1", len(config.InboundConfigs))
	}
	var settings map[string]any
	if err := json.Unmarshal(config.InboundConfigs[0].Settings, &settings); err != nil {
		t.Fatalf("unmarshal complete config inbound: %v", err)
	}
	gotClients := settings["clients"].([]any)
	want := []struct {
		email       string
		clientLimit float64
	}{
		{"default@test", 375000},
		{"override@test", 1125000},
	}
	for i, tc := range want {
		user := gotClients[i].(map[string]any)
		if user["email"] != xray.ScopedEmail("normalized-in", tc.email) || user["downlink_speed_limit"] != tc.clientLimit {
			t.Errorf("client %d settings = %v, want scoped email %q and per-client speed %v", i, user, tc.email, tc.clientLimit)
		}
		if user["rate_limit_group"] != "inbound:normalized-in" || user["downlink_group_speed_limit"] != float64(1875000) {
			t.Errorf("client %d group settings = %v, want inbound group cap 1875000 bytes/sec", i, user)
		}
	}
}

func TestXrayServiceTryHotApplyRestartsForNativeRateChange(t *testing.T) {
	oldConfig := &xray.Config{InboundConfigs: []xray.InboundConfig{{Tag: "in", Settings: []byte(`{"clients":[{"email":"e","downlink_speed_limit":100,"rate_limit_group":"inbound:in","downlink_group_speed_limit":500}]}`)}}}
	newConfig := &xray.Config{InboundConfigs: []xray.InboundConfig{{Tag: "in", Settings: []byte(`{"clients":[{"email":"e","downlink_speed_limit":200,"rate_limit_group":"inbound:in","downlink_group_speed_limit":500}]}`)}}}
	process := xray.NewTestProcess(oldConfig, t.TempDir()+"/config.json")
	if (&XrayService{}).tryHotApply(process, newConfig) {
		t.Fatal("tryHotApply = true; changed native limits must restart Xray to cap existing sessions")
	}
}

func TestUpdateXrayRejectsUnsupportedNativeRateLimitVersions(t *testing.T) {
	db := initTrafficTestDB(t)
	inbound := model.Inbound{UserId: 1, Enable: true, Protocol: model.VLESS, Tag: "limited", Port: 443, Settings: `{"clients":[]}`, InboundDownLimit: 10}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create limited inbound: %v", err)
	}
	for _, version := range []string{"v26.10.10-feat.1", "v26.10.10", "v26.10.9-feat.12"} {
		err := (&ServerService{}).UpdateXray(version)
		if err == nil || !strings.Contains(err.Error(), "require Rain-kl/Xray-core v26.10.10-feat.2 or newer") {
			t.Errorf("UpdateXray(%q) error = %v, want native rate-limit version rejection", version, err)
		}
	}
}

func TestNativeRateLimitValidationAllowsDisabledUnsupportedStoredCaps(t *testing.T) {
	db := initTrafficTestDB(t)

	// Disabled unsupported inbounds retain limits without applying them.
	disabled := &model.Inbound{
		UserId: 1, Enable: false, Protocol: model.TUIC, Tag: "stored-limit", Port: 444,
		Settings: `{"clients":[]}`, InboundDownLimit: 12, ClientDownLimit: 3,
	}
	created, _, err := (&InboundService{}).AddInbound(disabled)
	if err != nil {
		t.Fatalf("save disabled inbound with stored caps: %v", err)
	}
	var stored model.Inbound
	if err := db.First(&stored, created.Id).Error; err != nil {
		t.Fatalf("load disabled inbound: %v", err)
	}
	if stored.InboundDownLimit != 12 || stored.ClientDownLimit != 3 {
		t.Fatalf("stored limits = %d/%d, want 12/3 Mbps", stored.InboundDownLimit, stored.ClientDownLimit)
	}

	// An active client's stored cap is enforced as soon as its unsupported inbound is enabled.
	enabled := &model.Inbound{Enable: true, Protocol: model.TUIC, InboundDownLimit: 0}
	activeClient := model.Client{Enable: true, DownLimit: 5}
	err = validateNativeRateLimits(enabled, activeClient)
	if err == nil || !strings.Contains(err.Error(), "not supported for tuic") {
		t.Fatalf("active unsupported client limit error = %v, want TUIC support rejection", err)
	}
}

func TestHasLocalNativeRateLimitsIgnoresInactiveClientCaps(t *testing.T) {
	db := initTrafficTestDB(t)
	inbound := &model.Inbound{
		UserId: 1, Enable: true, Protocol: model.VLESS, Tag: "local-rate", Port: 445,
		Settings: `{"clients":[]}`,
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	client := &model.ClientRecord{Email: "inactive@example.test", DownLimit: 20, Enable: false}
	if err := db.Create(client).Error; err != nil {
		t.Fatalf("create disabled client: %v", err)
	}
	if err := db.Model(client).UpdateColumn("enable", false).Error; err != nil {
		t.Fatalf("disable client with schema default enabled: %v", err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: client.Id, InboundId: inbound.Id, DownLimit: 12}).Error; err != nil {
		t.Fatalf("attach disabled client with inbound override: %v", err)
	}

	if hasLocalNativeRateLimits() {
		t.Fatal("inactive client caps should not require a compatible local core")
	}
	client.Enable = true
	if err := db.Save(client).Error; err != nil {
		t.Fatalf("enable client: %v", err)
	}
	if !hasLocalNativeRateLimits() {
		t.Fatal("active client cap should require a compatible local core")
	}
	inbound.Enable = false
	if err := db.Save(inbound).Error; err != nil {
		t.Fatalf("disable inbound: %v", err)
	}
	if hasLocalNativeRateLimits() {
		t.Fatal("disabled inbound caps should not require a compatible local core")
	}
}
