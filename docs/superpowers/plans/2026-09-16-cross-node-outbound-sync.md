# Cross-Node Outbound Synchronization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable bi-directional synchronization and propagation of proxy outbounds between master and worker nodes with master precedence and deduplication by protocol+address+port.

**Architecture:** 
1. Build `outbound_sync` domain logic to classify proxy outbounds, extract normalized `protocol://address:port` keys, and merge/replace proxy outbounds in Xray template JSON while preserving local system outbounds.
2. Expose `/panel/api/server/outbounds` API endpoint (GET & POST) to read and apply proxy outbounds on remote nodes.
3. Update `Remote` runtime client to fetch/push proxy outbounds.
4. Hook master Xray setting update to mark worker nodes dirty; extend `NodeTrafficSyncJob` to pull new worker outbounds into the master template and push/reconcile master proxy outbounds to workers.
5. Update `FEATURE.md` and verify routes contract.

**Tech Stack:** Go 1.27, Gin, GORM, Xray JSON configuration.

## Global Constraints
- Maximum 2 lines per comment block.
- Follow Conventional Commits format (`type(scope): subject`).
- Only commit files explicitly modified for this task.
- Ensure `make lint` and `make test-go` pass.

---

### Task 1: Outbound Sync Domain Logic (`internal/web/service/outbound_sync.go` & test)

**Files:**
- Create: `internal/web/service/outbound_sync.go`
- Test: `internal/web/service/outbound_sync_test.go`

**Interfaces:**
- Produces:
  - `IsProxyOutbound(ob map[string]any) bool`
  - `ExtractOutboundEndpoints(ob map[string]any) []string`
  - `ExtractOutboundKeys(ob map[string]any) []string`
  - `MergeProxyOutboundsIntoTemplate(templateJSON string, incomingProxyOutbounds []map[string]any) (updatedTemplate string, addedCount int, err error)`
  - `ReplaceProxyOutboundsInTemplate(templateJSON string, masterProxyOutbounds []map[string]any) (updatedTemplate string, changed bool, err error)`
  - `GetProxyOutboundsFromTemplate(templateJSON string) ([]map[string]any, error)`

- [ ] **Step 1: Write the failing tests for outbound sync functions**
Write unit tests covering:
- Key extraction for vless, vmess, trojan, shadowsocks, hysteria, wireguard, socks, http.
- Exclusion of freedom, blackhole, dns.
- Merging incoming outbounds into template without duplicating existing endpoints.
- Replacing proxy outbounds in template while keeping system outbounds (`direct`, `blocked`, etc.).

- [ ] **Step 2: Run test to verify it fails**
Run: `go test -v ./internal/web/service -run TestOutboundSync`
Expected: FAIL due to undefined functions.

- [ ] **Step 3: Implement `outbound_sync.go`**
Implement key extraction, deduplication, template extraction, and merging/replacement helpers.

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/web/service -run TestOutboundSync`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/web/service/outbound_sync.go internal/web/service/outbound_sync_test.go
git commit -m "feat(outbound): add outbound deduplication and template sync helpers"
```

---

### Task 2: Server Controller Outbounds API & Routes Contract

**Files:**
- Modify: `internal/web/controller/server.go`
- Modify: `frontend/src/pages/api-docs/endpoints.ts`
- Test: `internal/web/routes_contract_test.go`

**Interfaces:**
- Consumes: `GetProxyOutboundsFromTemplate`, `ReplaceProxyOutboundsInTemplate` from Task 1.
- Produces:
  - `GET /panel/api/server/outbounds`: returns JSON array of current proxy outbounds.
  - `POST /panel/api/server/outbounds`: receives JSON array of master proxy outbounds, replaces local proxy outbounds in template, saves setting, and reloads Xray if running.

- [ ] **Step 1: Register routes in `endpoints.ts`**
Add `GET /panel/api/server/outbounds` and `POST /panel/api/server/outbounds` to `frontend/src/pages/api-docs/endpoints.ts`.

- [ ] **Step 2: Add controller methods to `internal/web/controller/server.go`**
Add `getOutbounds` and `setOutbounds` in `ServerController` and register them in `initRouter`.

- [ ] **Step 3: Run routes contract test**
Run: `go test -v ./internal/web -run TestRouteRegistryContract`
Expected: PASS.

- [ ] **Step 4: Commit**
```bash
git add internal/web/controller/server.go frontend/src/pages/api-docs/endpoints.ts
git commit -m "feat(server): expose /server/outbounds API for node outbound sync"
```

---

### Task 3: Remote Runtime Methods & Node Dirty Helper

**Files:**
- Modify: `internal/web/runtime/remote.go`
- Modify: `internal/web/service/node.go`
- Test: `internal/web/service/node_dirty_test.go`

**Interfaces:**
- Consumes: `/panel/api/server/outbounds` endpoint.
- Produces:
  - `(r *Remote) FetchProxyOutbounds(ctx context.Context) ([]map[string]any, error)`
  - `(r *Remote) PushProxyOutbounds(ctx context.Context, outbounds []map[string]any) error`
  - `(s *NodeService) MarkAllNodesDirtyTx(tx *gorm.DB) error`
  - `(s *NodeService) MarkOtherNodesDirtyTx(tx *gorm.DB, exceptNodeID int) error`

- [ ] **Step 1: Write test for `MarkAllNodesDirtyTx` and `MarkOtherNodesDirtyTx`**
Test that updating dirty flag marks corresponding node rows.

- [ ] **Step 2: Implement `MarkAllNodesDirtyTx` and `MarkOtherNodesDirtyTx` in `node.go`**
Add helper methods in `NodeService`.

- [ ] **Step 3: Implement `FetchProxyOutbounds` and `PushProxyOutbounds` on `*runtime.Remote`**
Add HTTP GET and POST calls to `panel/api/server/outbounds` in `internal/web/runtime/remote.go`.

- [ ] **Step 4: Run test to verify it passes**
Run: `go test -v ./internal/web/service -run TestNodeDirty`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/web/runtime/remote.go internal/web/service/node.go internal/web/service/node_dirty_test.go
git commit -m "feat(runtime): add remote proxy outbound fetch/push and batch dirty marking"
```

---

### Task 4: Master Outbound Change Hook & Sync Job Integration

**Files:**
- Modify: `internal/web/service/xray_setting.go`
- Modify: `internal/web/job/node_traffic_sync_job.go`
- Test: `internal/web/service/outbound_node_sync_test.go`

**Interfaces:**
- Consumes: `NodeService.MarkAllNodesDirtyTx`, `Remote.FetchProxyOutbounds`, `Remote.PushProxyOutbounds`, `MergeProxyOutboundsIntoTemplate`, `ReplaceProxyOutboundsInTemplate`.
- Behavior:
  - When master saves Xray settings (`SaveXraySetting` or `updateSetting`), if template outbounds changed, mark all nodes dirty.
  - In `NodeTrafficSyncJob.syncOne`:
    - When node is dirty, push master's proxy outbounds to the worker via `PushProxyOutbounds`.
    - Also, fetch worker's proxy outbounds; if worker has proxy outbounds not present in master, merge them into master's `xrayTemplateConfig`, mark other nodes dirty, and apply to master Xray.

- [ ] **Step 1: Write integration test for outbound node sync**
Write test `TestOutboundNodeSync_AdoptAndReconcile` verifying:
- Master adopts new outbound reported by worker.
- Master pushes its full set of proxy outbounds to worker upon reconcile.

- [ ] **Step 2: Hook master Xray setting save to mark all nodes dirty**
In `XraySettingService.SaveXraySetting`, mark nodes dirty if proxy outbounds changed.

- [ ] **Step 3: Integrate outbound pull/adopt and push/reconcile in `node_traffic_sync_job.go`**
Add `syncNodeOutbounds` during `syncOne` when communicating with worker.

- [ ] **Step 4: Run tests**
Run: `go test -v ./internal/web/service -run TestOutboundNodeSync`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/web/service/xray_setting.go internal/web/job/node_traffic_sync_job.go internal/web/service/outbound_node_sync_test.go
git commit -m "feat(job): integrate cross-node outbound adoption and reconciliation"
```

---

### Task 5: Documentation & Full Verification

**Files:**
- Modify: `FEATURE.md`

- [ ] **Step 1: Update `FEATURE.md`**
Add feature description adhering to repository standards:
"新增主从节点出站双向同步与扩散功能：支持主控台配置的出站自动下发到所有从节点，从节点新增的出站自动汇聚到主控台并全网扩散；基于协议+地址+端口去重，主控具备冲突优先权与删除同步能力。"

- [ ] **Step 2: Run verification**
Run: `make test-go`
Verify that all Go tests pass without issues.

- [ ] **Step 3: Commit**
```bash
git add FEATURE.md
git commit -m "docs: document cross-node outbound synchronization feature"
```
