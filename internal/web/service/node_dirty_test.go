package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// While a node is config-dirty (a local edit committed before it could be
// mirrored to the node), the traffic pull must not overwrite the central
// inbound's config columns from the node's stale snapshot — only traffic
// counters may advance. Otherwise a reconnecting node reverts the edit.
func TestSetRemoteTraffic_DirtyPreservesConfig(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	node := &model.Node{Name: "n1", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	id := node.Id

	const desiredSettings = `{"clients":[{"email":"a@x"}]}`
	central := &model.Inbound{
		UserId:   1,
		NodeID:   &id,
		Tag:      "in-443-tcp",
		Enable:   true,
		Port:     443,
		Protocol: model.VLESS,
		Settings: desiredSettings,
	}
	if err := db.Create(central).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	snap := &runtime.TrafficSnapshot{
		Inbounds: []*model.Inbound{{
			Tag:      "in-443-tcp",
			Enable:   true,
			Port:     443,
			Protocol: model.VLESS,
			Settings: `{"clients":[{"email":"b@x"}]}`,
			Up:       500,
			Down:     700,
		}},
	}

	svc := InboundService{}
	if _, err := svc.setRemoteTrafficLocked(id, snap, true, false); err != nil {
		t.Fatalf("setRemoteTrafficLocked dirty: %v", err)
	}

	var got model.Inbound
	if err := db.First(&got, central.Id).Error; err != nil {
		t.Fatalf("reload inbound: %v", err)
	}
	if got.Settings != desiredSettings {
		t.Fatalf("dirty pull overwrote settings: want %q got %q", desiredSettings, got.Settings)
	}
	if got.Up != 500 || got.Down != 700 {
		t.Fatalf("traffic counters not applied while dirty: up=%d down=%d", got.Up, got.Down)
	}
}

func TestSetRemoteTraffic_MissingDisabledInboundIsNotSwept(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()
	node := &model.Node{Name: "disabled-snapshot", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(node).Error; err != nil {
		t.Fatal(err)
	}
	disabled := &model.Inbound{
		UserId: 1, NodeID: &node.Id, Tag: "disabled", Enable: false,
		Port: 24443, Protocol: model.VLESS, Settings: `{"clients":[]}`,
	}
	reported := &model.Inbound{
		UserId: 1, NodeID: &node.Id, Tag: "reported", Enable: true,
		Port: 24444, Protocol: model.VLESS, Settings: `{"clients":[]}`,
	}
	if err := db.Create(disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(reported).Error; err != nil {
		t.Fatal(err)
	}
	snap := &runtime.TrafficSnapshot{Inbounds: []*model.Inbound{{
		Tag: reported.Tag, Enable: true,
		Port: reported.Port, Protocol: reported.Protocol, Settings: reported.Settings,
	}}}
	if _, err := (&InboundService{}).setRemoteTrafficLocked(node.Id, snap, false, false); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.Inbound{}).Where("id=?", disabled.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("disabled inbound rows=%d, want 1", count)
	}
}

// Deleting a *disabled* client attached to a node inbound must still propagate
// to the node. The node's own DB carries the (disabled) client, so the central
// panel has to mark the node dirty (→ reconcile) instead of dropping the delete
// and letting the next traffic snapshot resurrect the client. Regression for
// the enable-flag gate that used to skip the node path entirely (#5352).
func TestDelInboundClientByEmail_DisabledNodeClientMarksDirty(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	// Offline node so nodePushPlan reports dirty without needing a live runtime.
	node := &model.Node{Name: "n1", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "offline"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	id := node.Id

	central := &model.Inbound{
		UserId:   1,
		NodeID:   &id,
		Tag:      "in-443-tcp",
		Enable:   true,
		Port:     443,
		Protocol: model.VLESS,
		Settings: `{"clients":[{"email":"a@x","enable":false}]}`,
	}
	if err := db.Create(central).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	inboundSvc := &InboundService{}
	clientSvc := &ClientService{}
	if _, err := clientSvc.DelInboundClientByEmail(inboundSvc, central.Id, "a@x", false, false); err != nil {
		t.Fatalf("DelInboundClientByEmail: %v", err)
	}

	if _, _, dirty, _, err := (&NodeService{}).NodeSyncState(id); err != nil {
		t.Fatalf("NodeSyncState: %v", err)
	} else if !dirty {
		t.Fatal("deleting a disabled node client must mark the node dirty (#5352)")
	}
}

// An online, enabled node that is merely config-dirty must NOT be reported as
// pending: every node-backed edit marks the node dirty as the reconcile
// self-heal marker, so keying the "saved, node offline, will sync" toast off
// the dirty flag fired it on every save to a healthy online node.
func TestIsNodePending_OnlineDirtyNodeIsNotPending(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	node := &model.Node{Name: "n1", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	nodeSvc := NodeService{}
	if nodeSvc.IsNodePending(node.Id) {
		t.Fatal("a clean online node must not be pending")
	}
	if err := nodeSvc.MarkNodeDirty(node.Id); err != nil {
		t.Fatalf("MarkNodeDirty: %v", err)
	}
	if nodeSvc.IsNodePending(node.Id) {
		t.Fatal("an online, enabled node must not be pending just because it is config-dirty")
	}
}

// Offline or disabled nodes are genuinely deferred and must report pending so
// the "saved, node offline, will sync" toast still surfaces for them.
func TestIsNodePending_OfflineOrDisabledIsPending(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	offline := &model.Node{Name: "off", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "offline"}
	disabled := &model.Node{Name: "dis", Address: "127.0.0.1", Port: 2097, ApiToken: "tok", Enable: false, Status: "online"}
	for _, n := range []*model.Node{offline, disabled} {
		if err := db.Create(n).Error; err != nil {
			t.Fatalf("create node %s: %v", n.Name, err)
		}
	}
	// Node.Enable carries gorm default:true, so Create({Enable:false}) persists
	// TRUE — force the column off to actually exercise the disabled path.
	if err := db.Model(&model.Node{}).Where("id = ?", disabled.Id).Update("enable", false).Error; err != nil {
		t.Fatalf("force-disable node: %v", err)
	}

	nodeSvc := NodeService{}
	if !nodeSvc.IsNodePending(offline.Id) {
		t.Fatal("an offline node must be pending")
	}
	if !nodeSvc.IsNodePending(disabled.Id) {
		t.Fatal("a disabled node must be pending")
	}
}

// ClearNodeDirty must be a compare-and-swap on config_dirty_at so a concurrent
// edit that re-dirties the node during a reconcile is not silently cleared.
func TestNodeDirty_ClearIsCASOnDirtyAt(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	node := &model.Node{Name: "n2", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	nodeSvc := NodeService{}
	if err := nodeSvc.MarkNodeDirty(node.Id); err != nil {
		t.Fatalf("MarkNodeDirty: %v", err)
	}
	_, _, dirty, dirtyAt, err := nodeSvc.NodeSyncState(node.Id)
	if err != nil {
		t.Fatalf("NodeSyncState: %v", err)
	}
	if !dirty {
		t.Fatal("node should be dirty after MarkNodeDirty")
	}

	if err := nodeSvc.ClearNodeDirty(node.Id, dirtyAt-1); err != nil {
		t.Fatalf("ClearNodeDirty stale token: %v", err)
	}
	if _, _, stillDirty, _, _ := nodeSvc.NodeSyncState(node.Id); !stillDirty {
		t.Fatal("stale-token clear must not clear the dirty flag")
	}

	if err := nodeSvc.ClearNodeDirty(node.Id, dirtyAt); err != nil {
		t.Fatalf("ClearNodeDirty matching token: %v", err)
	}
	if _, _, stillDirty, _, _ := nodeSvc.NodeSyncState(node.Id); stillDirty {
		t.Fatal("matching-token clear must clear the dirty flag")
	}
}

func TestMarkNodeDirtyTxRollsBackWithTransaction(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	node := &model.Node{Name: "n3", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, Status: "online"}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	nodeSvc := NodeService{}
	rollbackErr := errors.New("force rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := nodeSvc.MarkNodeDirtyTx(tx, node.Id); err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback tx: got %v want %v", err, rollbackErr)
	}
	if _, _, dirty, _, err := nodeSvc.NodeSyncState(node.Id); err != nil {
		t.Fatalf("NodeSyncState after rollback: %v", err)
	} else if dirty {
		t.Fatal("dirty flag escaped a rolled-back transaction")
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		return nodeSvc.MarkNodeDirtyTx(tx, node.Id)
	}); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if _, _, dirty, _, err := nodeSvc.NodeSyncState(node.Id); err != nil {
		t.Fatalf("NodeSyncState after commit: %v", err)
	} else if !dirty {
		t.Fatal("dirty flag should commit with its transaction")
	}
}

// Editing a node must mark it config-dirty so the next traffic-sync tick
// reconciles (pushes the panel's inbounds to the remote) before pulling a
// snapshot. Without the dirty flag, re-pointing a node to a fresh server
// makes the orphan sweep delete every central inbound absent from the empty
// snapshot (#5461).
func TestNodeService_UpdateMarksNodeDirty(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	node := &model.Node{
		Name:     "n1",
		Address:  "10.0.0.1",
		Port:     2096,
		ApiToken: "tok",
		Enable:   true,
		Status:   "online",
	}
	if err := db.Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	edited := &model.Node{
		Name:     node.Name,
		Address:  "10.0.0.2",
		Port:     2097,
		ApiToken: node.ApiToken,
		Enable:   true,
	}
	nodeSvc := NodeService{}
	if err := nodeSvc.Update(node.Id, edited); err != nil {
		t.Fatalf("Update: %v", err)
	}

	_, _, dirty, _, err := nodeSvc.NodeSyncState(node.Id)
	if err != nil {
		t.Fatalf("NodeSyncState: %v", err)
	}
	if !dirty {
		t.Fatal("Update must mark the node config-dirty so sync reconciles before snapshot sweep (#5461)")
	}

	var got model.Node
	if err := db.First(&got, node.Id).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if got.Address != "10.0.0.2" || got.Port != 2097 {
		t.Fatalf("node row not updated: address=%q port=%d", got.Address, got.Port)
	}
}

// TestNodeDirty_MarkAllNodesDirtyTx verifies all enabled nodes are marked dirty.
func TestNodeDirty_MarkAllNodesDirtyTx(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	nodes := []*model.Node{
		{Name: "n1", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
		{Name: "n2", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
		{Name: "n3", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
	}
	for _, n := range nodes {
		if err := db.Create(n).Error; err != nil {
			t.Fatalf("create node %s: %v", n.Name, err)
		}
	}
	if err := db.Model(nodes[2]).Update("enable", false).Error; err != nil {
		t.Fatalf("disable node 3: %v", err)
	}

	svc := &NodeService{}
	if err := svc.MarkAllNodesDirtyTx(nil); err != nil {
		t.Fatalf("MarkAllNodesDirtyTx: %v", err)
	}

	var results []model.Node
	if err := db.Order("id asc").Find(&results).Error; err != nil {
		t.Fatalf("query nodes: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(results))
	}

	if !results[0].ConfigDirty || results[0].ConfigDirtyAt <= 0 {
		t.Errorf("node 1 should be dirty with timestamp, got dirty=%v, at=%d", results[0].ConfigDirty, results[0].ConfigDirtyAt)
	}
	if !results[1].ConfigDirty || results[1].ConfigDirtyAt <= 0 {
		t.Errorf("node 2 should be dirty with timestamp, got dirty=%v, at=%d", results[1].ConfigDirty, results[1].ConfigDirtyAt)
	}
	if results[2].ConfigDirty || results[2].ConfigDirtyAt != 0 {
		t.Errorf("node 3 (disabled) should not be dirty, got dirty=%v, at=%d", results[2].ConfigDirty, results[2].ConfigDirtyAt)
	}
}

// TestNodeDirty_MarkOtherNodesDirtyTx verifies all other enabled nodes are marked dirty.
func TestNodeDirty_MarkOtherNodesDirtyTx(t *testing.T) {
	setupConflictDB(t)
	db := database.GetDB()

	nodes := []*model.Node{
		{Name: "n1", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
		{Name: "n2", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
		{Name: "n3", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
		{Name: "n4", Enable: true, ConfigDirty: false, ConfigDirtyAt: 0},
	}
	for _, n := range nodes {
		if err := db.Create(n).Error; err != nil {
			t.Fatalf("create node %s: %v", n.Name, err)
		}
	}
	if err := db.Model(nodes[3]).Update("enable", false).Error; err != nil {
		t.Fatalf("disable node 4: %v", err)
	}

	svc := &NodeService{}
	if err := svc.MarkOtherNodesDirtyTx(nil, nodes[1].Id); err != nil {
		t.Fatalf("MarkOtherNodesDirtyTx: %v", err)
	}

	var results []model.Node
	if err := db.Order("id asc").Find(&results).Error; err != nil {
		t.Fatalf("query nodes: %v", err)
	}

	if !results[0].ConfigDirty || results[0].ConfigDirtyAt <= 0 {
		t.Errorf("node 1 should be dirty, got dirty=%v, at=%d", results[0].ConfigDirty, results[0].ConfigDirtyAt)
	}
	if results[1].ConfigDirty || results[1].ConfigDirtyAt != 0 {
		t.Errorf("node 2 (excepted) should not be dirty, got dirty=%v, at=%d", results[1].ConfigDirty, results[1].ConfigDirtyAt)
	}
	if !results[2].ConfigDirty || results[2].ConfigDirtyAt <= 0 {
		t.Errorf("node 3 should be dirty, got dirty=%v, at=%d", results[2].ConfigDirty, results[2].ConfigDirtyAt)
	}
	if results[3].ConfigDirty || results[3].ConfigDirtyAt != 0 {
		t.Errorf("node 4 (disabled) should not be dirty, got dirty=%v, at=%d", results[3].ConfigDirty, results[3].ConfigDirtyAt)
	}
}

// TestNodeDirty_RemoteProxyOutbounds verifies Remote fetch and push proxy outbounds methods.
func TestNodeDirty_RemoteProxyOutbounds(t *testing.T) {
	var postedBody []map[string]any
	var getMode string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != "/panel/api/server/outbounds" {
			http.NotFound(w, req)
			return
		}
		switch req.Method {
		case http.MethodGet:
			if getMode == "empty" {
				_, _ = w.Write([]byte(`{"success":true,"obj":null}`))
			} else {
				_, _ = w.Write([]byte(`{"success":true,"obj":[{"tag":"out-1","protocol":"shadowsocks"}]}`))
			}
		case http.MethodPost:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read post body: %v", err)
			}
			if err := json.Unmarshal(body, &postedBody); err != nil {
				t.Fatalf("unmarshal post body: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse srv url: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())
	node := &model.Node{
		Id:                  1,
		Name:                "test-node",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "dummy-token",
		Enable:              true,
		AllowPrivateAddress: true,
	}

	r := runtime.NewRemote(node, nil)
	ctx := context.Background()

	// Normal fetch.
	getMode = "normal"
	outbounds, err := r.FetchProxyOutbounds(ctx)
	if err != nil {
		t.Fatalf("FetchProxyOutbounds: %v", err)
	}
	if len(outbounds) != 1 || outbounds[0]["tag"] != "out-1" {
		t.Fatalf("unexpected outbounds: %+v", outbounds)
	}

	// Empty fetch.
	getMode = "empty"
	emptyOutbounds, err := r.FetchProxyOutbounds(ctx)
	if err != nil {
		t.Fatalf("FetchProxyOutbounds (empty): %v", err)
	}
	if emptyOutbounds != nil {
		t.Fatalf("expected nil outbounds, got %+v", emptyOutbounds)
	}

	// Push.
	toPush := []map[string]any{{"tag": "pushed-tag", "protocol": "vless"}}
	if err := r.PushProxyOutbounds(ctx, toPush); err != nil {
		t.Fatalf("PushProxyOutbounds: %v", err)
	}
	if len(postedBody) != 1 || postedBody[0]["tag"] != "pushed-tag" {
		t.Fatalf("unexpected posted body: %+v", postedBody)
	}
}

func TestNodeDirty_RemoteOutboundTags(t *testing.T) {
	var getMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != "/panel/api/server/outboundTags" {
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if getMode == "empty" {
			_, _ = w.Write([]byte(`{"success":true,"obj":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"obj":["direct","blocked","proxy-1"]}`))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse srv url: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())
	r := runtime.NewRemote(&model.Node{
		Id:                  1,
		Name:                "test-node",
		Scheme:              "http",
		Address:             u.Hostname(),
		Port:                port,
		BasePath:            "/",
		ApiToken:            "dummy-token",
		Enable:              true,
		AllowPrivateAddress: true,
	}, nil)
	ctx := context.Background()

	getMode = "normal"
	tags, err := r.FetchOutboundTags(ctx)
	if err != nil {
		t.Fatalf("FetchOutboundTags: %v", err)
	}
	if len(tags) != 3 || tags[0] != "direct" || tags[2] != "proxy-1" {
		t.Fatalf("unexpected tags: %+v", tags)
	}

	getMode = "empty"
	empty, err := r.FetchOutboundTags(ctx)
	if err != nil {
		t.Fatalf("FetchOutboundTags empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty tags, got %+v", empty)
	}
}
