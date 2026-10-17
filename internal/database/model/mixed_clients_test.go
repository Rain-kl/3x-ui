package model

import (
	"encoding/json"
	"testing"

	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/socks"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestMixedHotAddBuildsManagedAccounts(t *testing.T) {
	inbound := &Inbound{Tag: "mixed-hot", Protocol: Mixed, Port: 12345, Settings: `{"auth":"password","clients":[{"email":"billing","username":"login","password":"secret","enable":true},{"email":"disabled","username":"disabled","password":"secret","enable":false}]}`}
	wire, err := json.Marshal(inbound.GenXrayInboundConfig())
	if err != nil {
		t.Fatal(err)
	}
	var cfg conf.InboundDetourConfig
	if err := json.Unmarshal(wire, &cfg); err != nil {
		t.Fatal(err)
	}
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := built.ProxySettings.GetInstance()
	if err != nil {
		t.Fatal(err)
	}
	users := settings.(*socks.ServerConfig).Users
	if len(users) != 1 {
		t.Fatalf("hot-add includes disabled or loses users: %d", len(users))
	}
	account, err := users[0].GetTypedAccount()
	if err != nil {
		t.Fatal(err)
	}
	credentials := account.(*socks.Account)
	if credentials.Username != "login" || credentials.Password != "secret" || users[0].Email != xray.ScopedEmail(inbound.Tag, "billing") {
		t.Fatal("hot-add lost managed login or billing identity")
	}
	if inbound.Settings == string(inbound.GenXrayInboundConfig().Settings) {
		t.Fatal("database credentials were not projected")
	}
}
