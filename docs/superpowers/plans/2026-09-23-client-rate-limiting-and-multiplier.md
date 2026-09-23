# 客户端差异化限速与入站流量倍率 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `/panel/clients` 客户端全局下行限速与关联入站（节点）专属覆盖限速，支持入站流量计费倍率加权，并在 OrbStack 虚拟机中执行 5 客户端真实测速压测验证。

**Architecture:** 
1. 数据库模型扩展：`model.Inbound.TrafficRatio`（流量倍率）、`model.ClientRecord.DownLimit`（客户端全局限速）、`model.ClientInbound.DownLimit`（关联节点覆盖限速）及 `model.Client.DownLimit`（入站内嵌有效限速）；
2. 流控内核调度：`trafficshaper.Reconciler` 升级为支持每个客户端差异化 leaf class 挂载，并与入站整站限速形成 HTB 树状继承约束；
3. 流量加权计费：`InboundService.AddTraffic` 与 `NodeTrafficSyncJob` 将实际流量增量乘算 `TrafficRatio` 计入账本；
4. 前端交互：客户端表单新增「限速」Tab 动态配置全局限速与关联入站覆盖，入站表单配置流量倍率；
5. 实机压测：在 OrbStack 虚拟机中构建 5 客户端真实并发压测环境，验证节点级与用户级限速。

**Tech Stack:** Go 1.27, GORM, Gin, Linux TC (Traffic Control HTB), React 19, TypeScript, Ant Design 6.

## Global Constraints
- Do not touch Xray-core code.
- Go comments: 2 lines MAX per comment block. Make name carry meaning, focus on why.
- Conventional commits: `type(scope): subject`. Only stage and commit files changed for the task.
- Unthrottled traffic goes through `1:9999` class at 10Gbps default.
- All new i18n keys must be added to all 13 locale files in `internal/web/translation/`.

---

### Task 1: 数据模型与数据库迁移 (Data Models & Migrations)

**Files:**
- Modify: `internal/database/model/model.go`
- Modify: `internal/database/db.go`
- Test: `internal/database/client_rate_limit_model_test.go`

**Interfaces:**
- Produces:
  - `model.Inbound.TrafficRatio` (float64)
  - `model.ClientRecord.DownLimit` (int)
  - `model.ClientInbound.DownLimit` (int)
  - `model.Client.DownLimit` (int), `model.Client.DownLimitByInbound` (map[int]int)

- [ ] **Step 1: Write the failing test**

```go
package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestModelRateLimitAndTrafficRatioFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_fields.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	db := GetDB()
	ib := &model.Inbound{
		Remark:       "test-ratio",
		Port:         54321,
		Protocol:     model.VLESS,
		TrafficRatio: 1.5,
	}
	if err := db.Create(ib).Error; err != nil {
		t.Fatalf("Create Inbound failed: %v", err)
	}

	cr := &model.ClientRecord{
		Email:     "user@test.com",
		DownLimit: 100,
	}
	if err := db.Create(cr).Error; err != nil {
		t.Fatalf("Create ClientRecord failed: %v", err)
	}

	ci := &model.ClientInbound{
		ClientId:  cr.Id,
		InboundId: ib.Id,
		DownLimit: 50,
	}
	if err := db.Create(ci).Error; err != nil {
		t.Fatalf("Create ClientInbound failed: %v", err)
	}

	var loadedIb model.Inbound
	if err := db.First(&loadedIb, ib.Id).Error; err != nil || loadedIb.TrafficRatio != 1.5 {
		t.Fatalf("TrafficRatio = %v, want 1.5", loadedIb.TrafficRatio)
	}

	var loadedCi model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", cr.Id, ib.Id).First(&loadedCi).Error; err != nil || loadedCi.DownLimit != 50 {
		t.Fatalf("ClientInbound.DownLimit = %v, want 50", loadedCi.DownLimit)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/database -run TestModelRateLimitAndTrafficRatioFields`
Expected: FAIL (fields `TrafficRatio` or `DownLimit` not defined)

- [ ] **Step 3: Implement model changes and migrations**

1. In `internal/database/model/model.go`:
   - On `model.Inbound`: add `TrafficRatio float64` json:"trafficRatio" form:"trafficRatio" gorm:"column:traffic_ratio;default:1.0" validate:"omitempty,gte=0" example:"1.5"`
   - On `model.ClientRecord`: add `DownLimit int` json:"downLimit" form:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0" example:"100"`
   - On `model.ClientInbound`: add `DownLimit int` json:"downLimit" form:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0" example:"50"`
   - On `model.Client`: add `DownLimit int` json:"downLimit,omitempty"` and `DownLimitByInbound map[int]int` json:"downLimitByInbound,omitempty"`
2. In `internal/database/db.go`:
   - Ensure migration backfills default `traffic_ratio = 1.0` where `traffic_ratio <= 0`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/database -run TestModelRateLimitAndTrafficRatioFields`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/database/model/model.go internal/database/db.go internal/database/client_rate_limit_model_test.go
git commit -m "feat(database): add down_limit and traffic_ratio model fields"
```

---

### Task 2: 流控引擎支持客户端独立差异化限速 (TrafficShaper Multi-Client Limits)

**Files:**
- Modify: `internal/trafficshaper/types.go`
- Modify: `internal/trafficshaper/reconciler.go`
- Modify: `internal/trafficshaper/reconciler_test.go`

**Interfaces:**
- Consumes: `trafficshaper.Engine`, `trafficshaper.InboundRule`
- Produces:
  - `InboundRule.ClientLimits map[string]int`
  - `Reconciler.ApplyInbound`, `Reconciler.SyncClientIPs` with per-client dynamic limit calculation

- [ ] **Step 1: Write the failing test**

In `internal/trafficshaper/reconciler_test.go`:
```go
func TestReconciler_PerClientDifferentialDownLimits(t *testing.T) {
	mock := &mockCommandExecutor{}
	r := NewReconcilerWithExecutor("eth0", mock)
	ctx := context.Background()

	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10, // fallback default
		ClientLimits: map[string]int{
			"alice@test.com": 20, // custom client limit 20M
			"bob@test.com":   50, // custom client limit 50M
		},
	}
	if err := r.ApplyInbound(ctx, rule); err != nil {
		t.Fatalf("ApplyInbound failed: %v", err)
	}

	// Sync Alice with 20M limit
	if err := r.SyncClientIPs(ctx, 1, "alice@test.com", []string{"192.168.1.101"}); err != nil {
		t.Fatalf("SyncClientIPs alice: %v", err)
	}
	// Sync Bob with 50M limit
	if err := r.SyncClientIPs(ctx, 1, "bob@test.com", []string{"192.168.1.102"}); err != nil {
		t.Fatalf("SyncClientIPs bob: %v", err)
	}
	// Sync Charlie with fallback 10M limit
	if err := r.SyncClientIPs(ctx, 1, "charlie@test.com", []string{"192.168.1.103"}); err != nil {
		t.Fatalf("SyncClientIPs charlie: %v", err)
	}

	// Verify Alice class rate is 20mbit, Bob is 50mbit, Charlie is 10mbit
	assertCommandContains(t, mock.commands, "rate", "20mbit")
	assertCommandContains(t, mock.commands, "rate", "50mbit")
	assertCommandContains(t, mock.commands, "rate", "10mbit")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/trafficshaper -run TestReconciler_PerClientDifferentialDownLimits`
Expected: FAIL (`ClientLimits` not defined or individual limits not applied)

- [ ] **Step 3: Implement per-client differential limits in Reconciler**

1. In `internal/trafficshaper/types.go`:
   - Add `ClientLimits map[string]int` to `InboundRule`.
2. In `internal/trafficshaper/reconciler.go`:
   - Store `ClientLimits` in `inboundState`.
   - In `syncClientIPsLocked`:
     ```go
     clientLimit := state.rule.ClientDownLimit
     if custom, ok := state.rule.ClientLimits[email]; ok && custom > 0 {
         clientLimit = custom
     }
     if clientLimit <= 0 {
         return nil
     }
     rateStr, ceilStr := clientRateParams(state.rule.InboundDownLimit, clientLimit)
     ```
   - When `ApplyInbound` runs and updates `ClientLimits`, update existing client classes if their limit changed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/trafficshaper -run TestReconciler_PerClientDifferentialDownLimits`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/trafficshaper/types.go internal/trafficshaper/reconciler.go internal/trafficshaper/reconciler_test.go
git commit -m "feat(trafficshaper): support per-client differential bandwidth limits"
```

---

### Task 3: 业务服务层限速装配与流量倍率计费 (Service Layer & Traffic Accounting)

**Files:**
- Modify: `internal/web/service/client_crud.go`
- Modify: `internal/web/service/client_lookup.go`
- Modify: `internal/web/job/check_client_ip_job.go`
- Modify: `internal/web/service/inbound_traffic.go`
- Modify: `internal/web/service/inbound_node.go`
- Test: `internal/web/service/client_rate_limit_test.go`
- Test: `internal/web/service/traffic_ratio_test.go`

**Interfaces:**
- Produces:
  - Effective client rate limit calculation across `ClientRecord`, `ClientInbound`, and `Inbound`
  - Integration with `trafficshaper.InboundRule.ClientLimits`
  - Traffic multiplier applied in `InboundService.addClientTraffic` and `inbound_node.setRemoteTrafficLocked`

- [ ] **Step 1: Write the failing tests**

1. In `internal/web/service/client_rate_limit_test.go`:
   - Test `ComputeClientEffectiveLimit(email, inboundId)` matches priority:
     `client_inbound.down_limit > client.down_limit > inbound.client_down_limit`.
   - Test saving a client with `DownLimit` and `DownLimitByInbound` stores to `clients` table, `client_inbounds`, and `inbound.settings.clients`.
2. In `internal/web/service/traffic_ratio_test.go`:
   - Test that an inbound with `TrafficRatio: 2.0` results in client traffic delta being multiplied by 2 when added to `client_traffics`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/service -run "TestClientRateLimit|TestTrafficRatio"`
Expected: FAIL

- [ ] **Step 3: Implement service layer rate limiting & traffic ratio**

1. In `client_crud.go`:
   - In `Create` / `Update`:
     - Save `client.DownLimit` into `model.ClientRecord`.
     - Save `client.DownLimitByInbound[ibId]` into `model.ClientInbound`.
     - When constructing `model.Client` for `Inbound.Settings["clients"]`, set `DownLimit` to the resolved effective limit for that inbound.
2. In `client_lookup.go`:
   - Add helper `ClientLimitsByInbound(inboundId int) map[string]int` to query all clients attached to this inbound and return their effective limit.
3. In `check_client_ip_job.go`:
   - When registering inbounds and calling `ApplyInbound`, populate `InboundRule.ClientLimits` using `ClientLimitsByInbound(ib.Id)`.
4. In `inbound_traffic.go`:
   - In `addClientTraffic`:
     ```go
     if ratio > 0 && ratio != 1.0 {
         t.Up = int64(float64(t.Up) * ratio)
         t.Down = int64(float64(t.Down) * ratio)
     }
     ```
5. In `inbound_node.go`:
   - In `setRemoteTrafficLocked`:
     ```go
     if c.TrafficRatio > 0 && c.TrafficRatio != 1.0 {
         deltaUp = int64(float64(deltaUp) * c.TrafficRatio)
         deltaDown = int64(float64(deltaDown) * c.TrafficRatio)
     }
     ```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/web/service -run "TestClientRateLimit|TestTrafficRatio"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/service/client_crud.go internal/web/service/client_lookup.go internal/web/job/check_client_ip_job.go internal/web/service/inbound_traffic.go internal/web/service/inbound_node.go internal/web/service/client_rate_limit_test.go internal/web/service/traffic_ratio_test.go
git commit -m "feat(service): wire client rate limiting and inbound traffic ratio"
```

---

### Task 4: 前端交互与 API 契约对齐 (Frontend UI, API & Schema Codegen)

**Files:**
- Modify: `tools/openapigen/main.go`
- Modify: `frontend/src/schemas/client.ts`
- Modify: `frontend/src/schemas/inbound.ts`
- Modify: `frontend/src/pages/clients/ClientFormModal.tsx`
- Modify: `frontend/src/pages/clients/ClientsPage.tsx`
- Modify: `frontend/src/pages/inbounds/InboundModal.tsx`
- Modify: `internal/web/translation/*.json` (13 files)

**Interfaces:**
- Produces:
  - Frontend TypeScript/Zod schemas with `downLimit`, `downLimitByInbound`, and `trafficRatio`
  - Dedicated "限速" tab in `ClientFormModal.tsx`
  - "流量计费倍率" input in `InboundModal.tsx`
  - Rate limit badges in `ClientsPage.tsx`
  - 13-locale i18n keys

- [ ] **Step 1: Update OpenAPIGen and regenerate schemas**

1. Ensure new struct fields in `model.Inbound`, `model.ClientRecord`, and `model.Client` are allowed in `tools/openapigen/main.go`.
2. Run `make gen` to emit updated Zod schemas in `frontend/src/generated/`.
3. Update manual schemas in `frontend/src/schemas/client.ts` and `inbound.ts`.

- [ ] **Step 2: Add i18n keys to all 13 translation files**

In `internal/web/translation/`:
- `pages.clients.tabRateLimit`: "限速" / "Rate Limit" / etc.
- `pages.clients.downLimit`: "下行限速 (Mbps)" / "Downlink Limit (Mbps)"
- `pages.clients.downLimitDesc`: "客户端默认带宽上限，0 为不限速"
- `pages.clients.inboundRateLimitOverrides`: "关联节点限速覆盖"
- `pages.clients.inheritGlobalLimit`: "继承总限速 ({limit} Mbps)"
- `pages.inbounds.trafficRatio`: "流量计费倍率" / "Traffic Multiplier"
- `pages.inbounds.trafficRatioDesc`: "扣除客户端配额时的乘数，默认为 1.0"

- [ ] **Step 3: Implement ClientFormModal, InboundModal and ClientsPage UI**

1. In `ClientFormModal.tsx`:
   - Add `{ key: 'rateLimit', label: t('pages.clients.tabRateLimit'), children: ... }` to `Tabs`.
   - Render `downLimit` numeric input.
   - Render table of `attachedInbounds` with columns: Remark, Protocol/Port, Node Tag, and `downLimitByInbound` input.
2. In `InboundModal.tsx`:
   - Under the "限速" Tab, add `trafficRatio` InputNumber (`min={0}`, `step={0.1}`, `defaultValue={1.0}`).
3. In `ClientsPage.tsx`:
   - Display rate limit tag/badge in client list.

- [ ] **Step 4: Run frontend tests & dead-key verification**

Run: `cd frontend && pnpm test`
Expected: PASS (no dead i18n keys, schema tests pass).
Run: `make lint`
Expected: 0 issues.

- [ ] **Step 5: Commit**

```bash
git add frontend/ tools/openapigen/ internal/web/translation/
git commit -m "feat(frontend): add client rate limit tab and inbound traffic ratio controls"
```

---

### Task 5: OrbStack 虚拟机环境 5 客户端真实压测验证 (Benchmark Verification)

**Files:**
- Create: `test/benchmark/benchmark_5_clients.sh`
- Report: `docs/superpowers/reports/2026-09-23-5-clients-benchmark-report.md`

**Verification Scenario:**
- **Inbound**: Port `54321`, VLESS-Reality.
  - `inboundDownLimit = 100` (Mbps)
  - `clientDownLimit = 10` (Mbps)
- **5 Clients**:
  - `client1@test.com`: no custom limit -> expects 10 Mbps (inbound fallback)
  - `client2@test.com`: global limit 20 Mbps -> expects 20 Mbps
  - `client3@test.com`: global limit 30 Mbps -> expects 30 Mbps
  - `client4@test.com`: global limit 50 Mbps with node override 15 Mbps -> expects 15 Mbps (node override precedence)
  - `client5@test.com`: global limit 50 Mbps without override -> expects 50 Mbps
- **Concurrent Test**:
  - Launch 5 parallel curl clients through local proxy ports downloading large blobs over 15 seconds.
  - Measure individual throughput and aggregate throughput.

- [ ] **Step 1: Write the benchmark runner script**
- [ ] **Step 2: Execute benchmark in OrbStack VM**
- [ ] **Step 3: Verify results against expected limits ($\pm 10\%$) and total bandwidth $\le 100$ Mbps**
- [ ] **Step 4: Record report and commit**

```bash
git add test/benchmark/benchmark_5_clients.sh docs/superpowers/reports/2026-09-23-5-clients-benchmark-report.md
git commit -m "test(benchmark): verify 5 clients rate limiting in orbstack vm"
```
