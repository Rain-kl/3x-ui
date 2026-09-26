# 客户端单节点流量上限与用量监控实施计划 (Implementation Plan)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为客户端绑定的关联入站（节点）提供独立的流量上限配置（`total_gb`），在节点超额时仅熔断该节点连接而不影响其他节点，在订阅展示页以进度条呈现受限节点的已用量与配额，并在客户端编辑弹窗中新增「流量」Tab 集中管控。

**Architecture:** 
1. 数据库层面直接复用并扩展 `client_inbounds` 关系表，增加 `total_gb`（上限）、`up`（已用上行）、`down`（已用下行）3 个字段；
2. 5 秒流量轮询与从节点心跳记账时同事务同步累加 `client_inbounds`；超额时通过 `trafficRemoveUser` 仅从目标入站移除凭证；重置时同步清零并自愈恢复；
3. 订阅链接接口（`getInboundsBySubId`）不过滤超额节点；订阅网页（`/subscribe/:subId`）以 `<Progress>` 进度条展示受限节点已用与配额；客户端编辑弹窗提供「流量」Tab。

**Tech Stack:** Go 1.27, GORM (SQLite/PostgreSQL), Gin, React 19, Ant Design 6, TypeScript, Vite.

## Global Constraints

- Do NOT touch Xray-core code.
- Go/TS comments: 2 lines MAX per comment block.
- Ant Design 6 only.
- Conventional commits: `type(scope): subject`. Only stage and commit files changed for the specific task.
- Do NOT push to remote repository.
- Spec file: `docs/superpowers/specs/2026-09-26-client-node-traffic-limit-design.md`.

---

### Task 1: 数据模型扩展与数据库迁移 (Data Models & Migrations)

**Files:**
- Modify: `internal/database/model/model.go:1009-1020`
- Modify: `internal/database/db.go:120-140`
- Test: `internal/database/client_inbound_traffic_model_test.go`

**Interfaces:**
- Consumes: `model.ClientInbound`, `database.InitDB`
- Produces: `model.ClientInbound.TotalGB`, `model.ClientInbound.Up`, `model.ClientInbound.Down`, `model.Client.TotalGBByInbound`, `model.Client.InboundTraffics`, `model.ClientInboundTraffic`

- [ ] **Step 1: 编写数据模型与迁移失败测试**

创建 `internal/database/client_inbound_traffic_model_test.go`：
```go
package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientInboundTrafficColumns(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test-traffic.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = CloseDB() })

	ci := &model.ClientInbound{
		ClientId:  1,
		InboundId: 10,
		TotalGB:   50 * 1024 * 1024 * 1024,
		Up:        1024,
		Down:      2048,
	}
	if err := GetDB().Create(ci).Error; err != nil {
		t.Fatalf("Create ClientInbound failed: %v", err)
	}

	var found model.ClientInbound
	if err := GetDB().Where("client_id = ? AND inbound_id = ?", 1, 10).First(&found).Error; err != nil {
		t.Fatalf("Query ClientInbound failed: %v", err)
	}
	if found.TotalGB != 50*1024*1024*1024 {
		t.Errorf("TotalGB = %d, want %d", found.TotalGB, 50*1024*1024*1024)
	}
	if found.Up != 1024 || found.Down != 2048 {
		t.Errorf("Up/Down = %d/%d, want 1024/2048", found.Up, found.Down)
	}
}
```

- [ ] **Step 2: 运行测试以确认失败**

运行: `go test -v ./internal/database -run TestClientInboundTrafficColumns`
预期: 编译失败，提示 `ci.TotalGB undefined` 或 `field total_gb unknown`。

- [ ] **Step 3: 实现模型定义与迁移逻辑**

1. 修改 `internal/database/model/model.go`：
```go
type ClientInbound struct {
	ClientId     int    `json:"clientId" gorm:"primaryKey;column:client_id;index"`
	InboundId    int    `json:"inboundId" gorm:"primaryKey;column:inbound_id;index"`
	FlowOverride string `json:"flowOverride" gorm:"column:flow_override"`
	DownLimit    int    `json:"downLimit" form:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0" example:"50"`
	TotalGB      int64  `json:"totalGB" form:"totalGB" gorm:"column:total_gb;default:0" validate:"omitempty,gte=0" example:"53687091200"`
	Up           int64  `json:"up" form:"up" gorm:"column:up;default:0"`
	Down         int64  `json:"down" form:"down" gorm:"column:down;default:0"`
	CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}

type ClientInboundTraffic struct {
	InboundID int    `json:"inboundId"`
	Up        int64  `json:"up"`
	Down      int64  `json:"down"`
	Total     int64  `json:"total"`
	Used      int64  `json:"used"`
	Remained  int64  `json:"remained"`
	Depleted  bool   `json:"depleted"`
}
```
并在 `model.Client` 结构体中增加字段：
```go
	TotalGBByInbound map[int]int64 `json:"totalGBByInbound,omitempty"`
	InboundTraffics map[int]ClientInboundTraffic `json:"inboundTraffics,omitempty"`
```

2. 修改 `internal/database/db.go`，在迁移方法中追加 `client_inbounds` 新增列检测：
```go
	if !migrator.HasColumn(&model.ClientInbound{}, "total_gb") {
		if err := migrator.AddColumn(&model.ClientInbound{}, "total_gb"); err != nil {
			return err
		}
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "up") {
		if err := migrator.AddColumn(&model.ClientInbound{}, "up"); err != nil {
			return err
		}
	}
	if !migrator.HasColumn(&model.ClientInbound{}, "down") {
		if err := migrator.AddColumn(&model.ClientInbound{}, "down"); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: 运行测试验证通过**

运行: `go test -v ./internal/database -run TestClientInboundTrafficColumns`
预期: PASS

- [ ] **Step 5: 提交任务成果**

```bash
git add internal/database/model/model.go internal/database/db.go internal/database/client_inbound_traffic_model_test.go
git commit -m "feat(database): add traffic quota and usage columns to client_inbounds"
```

---

### Task 2: 业务服务层流量记账、熔断阻断与自愈重置 (Service Layer & Accounting)

**Files:**
- Modify: `internal/web/service/inbound_traffic.go:200-240`
- Modify: `internal/web/service/inbound_node.go:930-970`
- Modify: `internal/web/service/inbound_disable.go:80-160`
- Modify: `internal/web/service/client_crud.go:830-890`
- Modify: `internal/web/service/inbound_traffic_reset.go`
- Test: `internal/web/service/client_inbound_traffic_test.go`

**Interfaces:**
- Consumes: `model.ClientInbound`, `s.addClientTraffic`, `s.disableInvalidClients`, `s.setRemoteTrafficLocked`
- Produces: `client_inbounds` 双向记账、超额节点独立熔断、重置自愈

- [ ] **Step 1: 编写记账、熔断与重置的单元测试**

创建 `internal/web/service/client_inbound_traffic_test.go`：
```go
package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestClientInboundTraffic_AccountingAndDepletion(t *testing.T) {
	db := initTrafficTestDB(t)
	svc := &InboundService{}

	// 创建 Inbound A 和 Inbound B
	ibA := &model.Inbound{UserId: 1, Tag: "node-a", Enable: true, Port: 41001, Protocol: model.VLESS, Settings: `{"clients":[]}`}
	ibB := &model.Inbound{UserId: 1, Tag: "node-b", Enable: true, Port: 41002, Protocol: model.VLESS, Settings: `{"clients":[]}`}
	if err := db.Create(ibA).Error; err != nil {
		t.Fatalf("create ibA: %v", err)
	}
	if err := db.Create(ibB).Error; err != nil {
		t.Fatalf("create ibB: %v", err)
	}

	// 创建客户端，总限流 200GB (200 << 30)
	clientRecord := &model.ClientRecord{
		Email:   "user@test.com",
		TotalGB: 200 << 30,
		Enable:  true,
	}
	if err := db.Create(clientRecord).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	// 关联 A (无单独限额) 和 B (单限 50GB)
	ciA := &model.ClientInbound{ClientId: clientRecord.Id, InboundId: ibA.Id, TotalGB: 0}
	ciB := &model.ClientInbound{ClientId: clientRecord.Id, InboundId: ibB.Id, TotalGB: 50 << 30}
	if err := db.Create(ciA).Error; err != nil {
		t.Fatalf("create ciA: %v", err)
	}
	if err := db.Create(ciB).Error; err != nil {
		t.Fatalf("create ciB: %v", err)
	}

	if err := svc.AddClientStat(db, ibA.Id, &model.Client{Email: "user@test.com", Enable: true}); err != nil {
		t.Fatalf("AddClientStat: %v", err)
	}

	// 模拟 B 节点上报 51GB 流量 (51 << 30)
	traffics := []*xray.ClientTraffic{
		{InboundId: ibB.Id, Email: "user@test.com", Up: 25 << 30, Down: 26 << 30},
	}
	if _, _, err := svc.AddTraffic(nil, traffics); err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}

	// 验证 client_inbounds B 节点记账
	var rowB model.ClientInbound
	if err := db.Where("client_id = ? AND inbound_id = ?", clientRecord.Id, ibB.Id).First(&rowB).Error; err != nil {
		t.Fatalf("query ciB: %v", err)
	}
	if rowB.Up+rowB.Down != 51<<30 {
		t.Errorf("ciB used = %d, want %d", rowB.Up+rowB.Down, 51<<30)
	}

	// 验证全局流量表扣除了 51GB
	var globalRow xray.ClientTraffic
	if err := db.Where("email = ?", "user@test.com").First(&globalRow).Error; err != nil {
		t.Fatalf("query global: %v", err)
	}
	if globalRow.Up+globalRow.Down != 51<<30 {
		t.Errorf("global used = %d, want %d", globalRow.Up+globalRow.Down, 51<<30)
	}

	// 验证全局客户端并未禁用 (全局仍有 149GB)
	var cr model.ClientRecord
	if err := db.First(&cr, clientRecord.Id).Error; err != nil {
		t.Fatalf("query cr: %v", err)
	}
	if !cr.Enable {
		t.Errorf("cr.Enable = false, want true (client must remain enabled globally)")
	}
}
```

- [ ] **Step 2: 运行测试以确认失败**

运行: `go test -v ./internal/web/service -run TestClientInboundTraffic_AccountingAndDepletion`
预期: FAIL（`ciB used = 0`，尚未在 `addClientTraffic` 中累加 `client_inbounds`）。

- [ ] **Step 3: 实现记账、熔断与重置逻辑**

1. 修改 `internal/web/service/inbound_traffic.go`：
在 `addClientTraffic` 中，同步累加 `client_inbounds`：
```go
		if ct.InboundId > 0 {
			// 定位 client_id 并原子累加关联入站流量
			_ = tx.Exec(
				fmt.Sprintf(
					`UPDATE client_inbounds SET up = %s, down = %s
					 WHERE inbound_id = ? AND client_id = (SELECT id FROM clients WHERE email = ? LIMIT 1)`,
					database.ClampedAddExpr("up"),
					database.ClampedAddExpr("down"),
				),
				t.Up, t.Down, ibId, ct.Email,
			).Error
		}
```
2. 修改 `internal/web/service/inbound_node.go`：
在 `setRemoteTrafficLocked` 的 `deltaUp, deltaDown` 写入处，同步累加从节点对应的 `client_inbounds`。
3. 修改 `internal/web/service/inbound_disable.go`：
在 `disableInvalidClients` 中增加单入站超额阻断判定：
```go
	// 检索达到独立节点配额的客户端关联
	type depletedInboundTarget struct {
		ClientId  int    `gorm:"column:client_id"`
		InboundId int    `gorm:"column:inbound_id"`
		Email     string `gorm:"column:email"`
	}
	var depletedInbounds []depletedInboundTarget
	_ = tx.Raw(`
		SELECT ci.client_id, ci.inbound_id, c.email
		FROM client_inbounds ci
		JOIN clients c ON c.id = ci.client_id
		WHERE ci.total_gb > 0 AND (ci.up + ci.down) >= ci.total_gb
	`).Scan(&depletedInbounds).Error
```
对属于 `depletedInbounds` 的条目调用 `markClientsDisabledInSettings` 并生成 `trafficRemoveUser`（本地）或 remote plan，仅阻断该入站，不修改 `clients.enable`。
4. 修改 `internal/web/service/client_crud.go`：
在创建/更新客户端时，持久化 `TotalGBByInbound map[int]int64` 至 `client_inbounds.total_gb`；在查询客户端详情时聚合填充 `InboundTraffics`。
5. 修改重置逻辑：在 `autoRenewClients` 和 `ResetClientTraffic` 中，执行 `UPDATE client_inbounds SET up = 0, down = 0 WHERE client_id = ?`，并联动将入站中的客户端恢复启用。

- [ ] **Step 4: 运行测试验证通过**

运行: `go test -v ./internal/web/service -run TestClientInboundTraffic_AccountingAndDepletion`
预期: PASS

- [ ] **Step 5: 提交任务成果**

```bash
git add internal/web/service/inbound_traffic.go internal/web/service/inbound_node.go internal/web/service/inbound_disable.go internal/web/service/client_crud.go internal/web/service/client_inbound_traffic_test.go
git commit -m "feat(service): implement dual-accounting and single-node traffic depletion"
```

---

### Task 3: 订阅服务与订阅展示页进度条呈现 (Subscription Page & Data Injection)

**Files:**
- Modify: `internal/sub/service.go:2920-2950,3070-3125`
- Modify: `internal/sub/controller.go:655-685`
- Modify: `frontend/src/pages/sub/subPageModel.ts`
- Modify: `frontend/src/pages/sub/SubPage.tsx`
- Modify: `frontend/src/pages/sub/SubHero.tsx`
- Test: `internal/sub/sub_limited_nodes_test.go`
- Test: `frontend/src/test/sub-limited-nodes.test.ts`

**Interfaces:**
- Consumes: `PageData`, `SubNodeLimit`, `window.__SUB_PAGE_DATA__`
- Produces: 订阅页面渲染受限节点进度条卡片

- [ ] **Step 1: 编写 Go 端 PageData 数据注入失败测试**

创建 `internal/sub/sub_limited_nodes_test.go`：
```go
package sub

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestBuildPageData_PopulatesLimitedNodes(t *testing.T) {
	dbDir := t.TempDir()
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	inbound := &model.Inbound{
		Remark:   "HongKong-VIP",
		Port:     38443,
		Protocol: model.VLESS,
		Tag:      "in-hk",
		Enable:   true,
	}
	database.GetDB().Create(inbound)

	client := &model.ClientRecord{
		Email: "subuser@test.com",
		SubID: "subtest123",
	}
	database.GetDB().Create(client)

	ci := &model.ClientInbound{
		ClientId:  client.Id,
		InboundId: inbound.Id,
		TotalGB:   50 * 1024 * 1024 * 1024,
		Up:        10 * 1024 * 1024 * 1024,
		Down:      15 * 1024 * 1024 * 1024,
	}
	database.GetDB().Create(ci)

	svc := NewSubService()
	page := svc.BuildPageData("subtest123", "sub.com", xray.ClientTraffic{Total: 200 << 30}, 0, []string{"vless://..."}, []string{"subuser@test.com"}, "", "", "", "/", "Title", "")

	if len(page.LimitedNodes) != 1 {
		t.Fatalf("expected 1 limited node, got %d", len(page.LimitedNodes))
	}
	node := page.LimitedNodes[0]
	if node.Name != "HongKong-VIP" {
		t.Errorf("node.Name = %q, want HongKong-VIP", node.Name)
	}
	if node.Percent < 49.0 || node.Percent > 51.0 {
		t.Errorf("node.Percent = %f, want ~50.0", node.Percent)
	}
}
```

- [ ] **Step 2: 运行测试以确认失败**

运行: `go test -v ./internal/sub -run TestBuildPageData_PopulatesLimitedNodes`
预期: FAIL（`page.LimitedNodes undefined`）。

- [ ] **Step 3: 实现 PageData 数据提取与注入**

1. 修改 `internal/sub/service.go`：
定义 `SubNodeLimit` 并在 `PageData` 中加入 `LimitedNodes []SubNodeLimit`：
```go
type SubNodeLimit struct {
	Name     string  `json:"name"`
	Used     string  `json:"used"`
	Total    string  `json:"total"`
	Remained string  `json:"remained"`
	Percent  float64 `json:"percent"`
	Depleted bool    `json:"depleted"`
}
```
在 `BuildPageData` 中查询该 subscriber 的 `client_inbounds WHERE total_gb > 0`，格式化构建 `LimitedNodes`。
2. 修改 `internal/sub/controller.go`：在 `subPageContext` 中映射 `"limitedNodes": page.LimitedNodes`。
3. 修改前端 `frontend/src/pages/sub/subPageModel.ts`：定义 `SubNodeLimit` 接口。
4. 修改 `frontend/src/pages/sub/SubPage.tsx`：当 `subData.limitedNodes` 存在且非空时，渲染「节点流量配额」卡片。
5. 编写/修改 `frontend/src/test/sub-limited-nodes.test.ts` 进行前端用例验证。

- [ ] **Step 4: 运行测试验证通过**

运行: `go test -v ./internal/sub -run TestBuildPageData_PopulatesLimitedNodes`
运行: `cd frontend && pnpm test`
预期: PASS

- [ ] **Step 5: 提交任务成果**

```bash
git add internal/sub/service.go internal/sub/controller.go internal/sub/sub_limited_nodes_test.go frontend/src/pages/sub/
git commit -m "feat(sub): display limited nodes with progress bar on subscription page"
```

---

### Task 4: 前端客户端管理弹窗「流量」Tab 界面交互 (Frontend UI & Forms)

**Files:**
- Modify: `frontend/src/pages/clients/ClientFormModal.tsx:870-920,1380-1480`
- Modify: `frontend/src/schemas/forms/client-form.ts`
- Modify: `internal/web/translation/*.json` (所有 13 个语言包)
- Codegen: `tools/openapigen`, `make gen`

**Interfaces:**
- Consumes: `totalGBByInbound`, `inboundTraffics`
- Produces: 客户端编辑弹窗中的「流量」Tab 与用量表格

- [ ] **Step 1: 扩展前端 Zod Schema 并生成 OpenAPI 类型**

1. 在 `frontend/src/schemas/forms/client-form.ts` 中增加：
```typescript
  totalGBByInbound: z.record(z.coerce.number(), z.number().min(0)).optional().default({}),
```
2. 运行 `make gen` 或 `cd frontend && pnpm run gen` 同步类型与契约。

- [ ] **Step 2: 在 ClientFormModal 中新增「流量」Tab**

1. 将 `totalGB`, `trafficReset`, `trafficResetDay` 从「基础信息」收纳至新增的「流量」Tab 顶部卡片。
2. 在「流量」Tab 下方渲染关联入站流量表格：
   - 节点 / 备注
   - 协议 / 端口
   - 已用流量（编辑模式下展示，使用 `formatTraffic` 格式化）
   - 节点流量上限（`InputNumber`，单位 GB，支持 `min={0}`，placeholder="跟随总配额"）
   - 剩余流量（计算并显示）
   - 状态徽标（`正常` / `已用尽阻断`）
3. 补全 13 种语言包的翻译键：
   - `pages.clients.tabTraffic`
   - `pages.clients.inboundTrafficLimits`
   - `pages.clients.nodeQuota`
   - `pages.clients.nodeUsed`
   - `pages.clients.nodeRemained`
   - `pages.clients.inheritGlobalQuota`
   - `pages.clients.depleted`

- [ ] **Step 3: 运行前端门禁测试与 Dead Keys 检查**

运行: `cd frontend && pnpm run test`
运行: `cd frontend && pnpm run lint && pnpm run typecheck`
预期: PASS，0 dead keys，0 lint errors。

- [ ] **Step 4: 提交任务成果**

```bash
git add frontend/src/ internal/web/translation/ tools/openapigen/
git commit -m "feat(frontend): add traffic tab and per-inbound quota table to client modal"
```

---

### Task 5: 综合门禁验证与二开特性登记 (Verification & FEATURE.md)

**Files:**
- Modify: `FEATURE.md`
- Test: 全局测试与真实流程测试

**Interfaces:**
- Consumes: 全模块改动
- Produces: 完备特性文档与全绿门禁

- [ ] **Step 1: 运行全量后端测试与代码检查**

运行: `go test -v ./internal/web/service -run ClientInboundTraffic`
运行: `golangci-lint run`
预期: PASS，0 issues。

- [ ] **Step 2: 运行全量前端验证**

运行: `cd frontend && pnpm test && pnpm run typecheck && pnpm run format:check`
预期: PASS。

- [ ] **Step 3: 登记 FEATURE.md**

在 [`FEATURE.md`](file:///Users/ryan/Code/Go/3x-ui/FEATURE.md) 中，按照规范追加：
```markdown
## 客户端单节点独立流量上限与用量监控
- **价值与场景**：在多节点混合运营场景中，不同节点成本差异巨大（如优质家宽/专线节点 vs 普通公网直连节点）。管理员需要为客户分配总流量套餐的同时，对高成本节点设置单独的使用上限，防止用户将总流量全部消耗在昂贵节点上；并在节点用尽时精准熔断单节点，不影响其他节点正常使用。
- **功能描述**：
  1. **单节点独立流量上限**：支持在客户端编辑弹窗的「流量」选项卡中，针对每个关联节点单独配置流量配额（如总配额 200GB，优质节点单独限额 50GB）。
  2. **精准单节点熔断与断流**：当客户端在特定节点流量用尽时，服务端 Xray 运行时仅将该客户端从该节点中注销断流，客户端在其他节点（如节点 A）连接与剩余配额依然完全正常。
  3. **订阅展示页进度条呈现**：订阅链接配置中保留节点，并在订阅信息展示网页端（`/subscribe/:subId`）以进度条形式直观展现各限流节点的已用流量、总配额与剩余流量。
  4. **全自动流量自愈与重置**：当客户端触发定时周期重置或管理员手动重置流量时，各节点已用量同步清零并自动恢复连接。
```

- [ ] **Step 4: 提交 FEATURE.md**

```bash
git add FEATURE.md
git commit -m "docs(feature): record client per-node traffic limit and monitoring"
```
