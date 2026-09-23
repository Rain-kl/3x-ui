# 入站与客户端带宽限速 (Inbound & Client Rate Limiting) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 3x-ui 入站节点提供基于 Linux 内核 `tc` HTB 的下行带宽限速能力，支持整入站限速与每客户端独立限速，实现近零 CPU 损耗且完全解耦于 Xray 核心版本。

**Architecture:** 通过在数据模型中持久化入站限速规则，在 Go 后端实现高内聚的 `internal/trafficshaper` 引擎管理 Linux `tc` 树状层级队列。结合已有的 `CheckClientIpJob` 动态追踪客户端在线 IP 并执行防抖增量对齐，在前端提供入站限速配置与醒目徽标展示。

**Tech Stack:** Go 1.27, Gin, GORM, Linux Traffic Control (`tc` / HTB / u32 / fq_codel), React 19, Ant Design 6, TypeScript, Zod.

## Global Constraints

- 严禁直接侵入或修改 Xray-core 上游代码，保证用户可自由切换任意官方 Xray 二进制版本。
- 注释规范：Go / TS 注释最多 2 行，聚焦 *Why*。
- 仅限制下行出口速率（VPS -> 客户端），未限速流量通过 `1:9999` 类物理满速放行，确保 SSH 与宿主机服务安全。
- 遵循 Conventional Commits：`type(scope): subject`。每次任务完成后仅提交当前任务涉及的文件。
- 前端新增或修改的 i18n 键必须同步增补全部 13 个 locale JSON 文件（`internal/web/translation/`）。
- 每次涉及 API 结构变动必须通过 `make gen` 同步契约并保证 `TestRouteRegistryContract` 通过。

---

### Task 1: 数据库模型扩展与迁移 (Model & Migration)

**Files:**
- Modify: `internal/database/model/model.go`
- Modify: `internal/database/db.go`
- Test: `internal/database/rate_limit_schema_test.go`

**Interfaces:**
- Consumes: GORM DB connection
- Produces: `Inbound.InboundDownLimit` (int), `Inbound.ClientDownLimit` (int)

- [ ] **Step 1: 编写数据模型与迁移测试**

```go
package database

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestInboundRateLimitSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_rate_limit.db")
	err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer func() { _ = CloseDB() }()

	inbound := &model.Inbound{
		Remark:           "test-rate-limit",
		Port:             18443,
		Protocol:         model.VLESS,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
		Tag:              "in-test-rl",
		Enable:           true,
	}

	if err := GetDB().Create(inbound).Error; err != nil {
		t.Fatalf("Create inbound with rate limits failed: %v", err)
	}

	var loaded model.Inbound
	if err := GetDB().First(&loaded, inbound.Id).Error; err != nil {
		t.Fatalf("Query inbound failed: %v", err)
	}

	if loaded.InboundDownLimit != 100 || loaded.ClientDownLimit != 10 {
		t.Fatalf("Unexpected rate limits: got inbound=%d client=%d, want 100 and 10",
			loaded.InboundDownLimit, loaded.ClientDownLimit)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test -v ./internal/database -run TestInboundRateLimitSchema`
Expected: FAIL (field InboundDownLimit not defined on model.Inbound)

- [ ] **Step 3: 扩展 Inbound 模型并添加迁移**

在 `internal/database/model/model.go` 的 `type Inbound struct` 中添加：
```go
// InboundDownLimit sets peak outbound bandwidth for this entire inbound in Mbps (0 = unlimited).
InboundDownLimit int `json:"inboundDownLimit" form:"inboundDownLimit" gorm:"column:inbound_down_limit;default:0" validate:"omitempty,gte=0" example:"100"`
// ClientDownLimit sets peak outbound bandwidth for each client on this inbound in Mbps (0 = unlimited).
ClientDownLimit int `json:"clientDownLimit" form:"clientDownLimit" gorm:"column:client_down_limit;default:0" validate:"omitempty,gte=0" example:"10"`
```

在 `internal/database/db.go` 的 `migrate()` 函数中增加列检查迁移：
```go
if !db.Migrator().HasColumn(&model.Inbound{}, "inbound_down_limit") {
    _ = db.Migrator().AddColumn(&model.Inbound{}, "inbound_down_limit")
}
if !db.Migrator().HasColumn(&model.Inbound{}, "client_down_limit") {
    _ = db.Migrator().AddColumn(&model.Inbound{}, "client_down_limit")
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test -v ./internal/database -run TestInboundRateLimitSchema`
Expected: PASS

- [ ] **Step 5: 提交更改**

```bash
git add internal/database/model/model.go internal/database/db.go internal/database/rate_limit_schema_test.go
git commit -m "feat(database): add rate limit fields to inbound model"
```

---

### Task 2: 流控核心引擎与 TC 命令执行器 (TrafficShaper Engine & Executor)

**Files:**
- Create: `internal/trafficshaper/types.go`
- Create: `internal/trafficshaper/executor.go`
- Create: `internal/trafficshaper/engine.go`
- Test: `internal/trafficshaper/engine_test.go`

**Interfaces:**
- Consumes: OS network interfaces, `tc` command line interface
- Produces: `Engine.Init()`, `Engine.Teardown()`, `Engine.IsSupported() bool`

- [ ] **Step 1: 编写引擎单元测试**

```go
package trafficshaper

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type mockExecutor struct {
	mu       sync.Mutex
	commands []string
}

func (m *mockExecutor) Execute(ctx context.Context, cmd string, args ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, cmd+" "+strings.Join(args, " "))
	return nil
}

func TestEngineInitAndTeardown(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)

	ctx := context.Background()
	if err := engine.Init(ctx); err != nil {
		t.Fatalf("engine.Init failed: %v", err)
	}

	if len(mock.commands) == 0 {
		t.Fatalf("expected tc commands executed on init, got none")
	}

	// Verify root HTB qdisc and default 9999 class created
	hasRoot := false
	hasDefaultClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "qdisc replace dev eth0 root handle 1: htb default 9999") {
			hasRoot = true
		}
		if strings.Contains(cmd, "class replace dev eth0 parent 1: classid 1:9999") {
			hasDefaultClass = true
		}
	}
	if !hasRoot || !hasDefaultClass {
		t.Errorf("missing root or default class in executed commands: %v", mock.commands)
	}

	if err := engine.Teardown(ctx); err != nil {
		t.Fatalf("engine.Teardown failed: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test -v ./internal/trafficshaper -run TestEngineInitAndTeardown`
Expected: FAIL (package trafficshaper not found)

- [ ] **Step 3: 实现 Executor、Types 与 Engine**

编写 `internal/trafficshaper/types.go`：
定义 `InboundRule`（ID, Port, InboundLimitMbps, ClientLimitMbps, ActiveIPs）。

编写 `internal/trafficshaper/executor.go`：
定义 `CommandExecutor` 接口，提供默认的 `SysExecutor`（封装 `os/exec.CommandContext`，带 3 秒超时限制）。

编写 `internal/trafficshaper/engine.go`：
实现 `Engine` 结构体：
- 自动探测默认网卡（若未指定，通过路由表获取）。
- `Init(ctx)`：清理旧规则并配置 `handle 1: htb default 9999` 及 `1:1` 总类。
- `Teardown(ctx)`：执行 `tc qdisc del dev <iface> root`。
- `IsSupported()`：检查 `runtime.GOOS == "linux"` 并探测 `tc` 命令。

- [ ] **Step 4: 运行测试验证通过**

Run: `go test -v ./internal/trafficshaper -run TestEngineInitAndTeardown`
Expected: PASS

- [ ] **Step 5: 提交更改**

```bash
git add internal/trafficshaper/
git commit -m "feat(trafficshaper): add tc engine and executor"
```

---

### Task 3: 增量状态对齐器与动态 IP 防抖 (Reconciler & Dynamic IP Sync)

**Files:**
- Create: `internal/trafficshaper/reconciler.go`
- Test: `internal/trafficshaper/reconciler_test.go`

**Interfaces:**
- Consumes: `Engine`, `InboundRule`
- Produces: `Reconciler.UpdateInbound(rule)`, `Reconciler.RemoveInbound(id)`, `Reconciler.SyncClientIPs(inboundId, email, ips)`

- [ ] **Step 1: 编写增量对齐与防抖测试**

```go
package trafficshaper

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReconcilerInboundAndClientSync(t *testing.T) {
	mock := &mockExecutor{}
	engine := NewEngineWithExecutor("eth0", mock)
	reconciler := NewReconciler(engine)

	ctx := context.Background()
	rule := InboundRule{
		InboundID:        1,
		Port:             443,
		InboundDownLimit: 100,
		ClientDownLimit:  10,
	}

	// Apply inbound rule
	reconciler.ApplyInbound(ctx, rule)

	// Verify inbound class created with 100mbit
	hasInboundClass := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class replace dev eth0 parent 1:1 classid 1:10 htb rate 100mbit") {
			hasInboundClass = true
		}
	}
	if !hasInboundClass {
		t.Fatalf("inbound class not found in commands: %v", mock.commands)
	}

	// Sync active client IP
	reconciler.SyncClientIPs(ctx, 1, "user1@example.com", []string{"192.168.1.100"})

	// Verify client class and filter created with 10mbit and fq_codel
	hasClientClass := false
	hasClientFilter := false
	for _, cmd := range mock.commands {
		if strings.Contains(cmd, "class replace dev eth0 parent 1:10") && strings.Contains(cmd, "rate 10mbit") {
			hasClientClass = true
		}
		if strings.Contains(cmd, "filter replace dev eth0") && strings.Contains(cmd, "match ip dst 192.168.1.100/32") {
			hasClientFilter = true
		}
	}
	if !hasClientClass || !hasClientFilter {
		t.Fatalf("client class/filter not created properly: %v", mock.commands)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test -v ./internal/trafficshaper -run TestReconcilerInboundAndClientSync`
Expected: FAIL (Reconciler not defined)

- [ ] **Step 3: 实现 Reconciler 增量对齐与调度逻辑**

编写 `internal/trafficshaper/reconciler.go`：
- Class ID 确定性编排算法：入站 Class 为 `1:${inboundId}0`，客户端子 Class 基于 Hash/自增分配。
- 端口过滤器：`tc filter replace dev <iface> protocol ip parent 1:0 prio 10 u32 match ip sport <port> 0xffff flowid 1:${inboundId}0`。
- IP 过滤器：`tc filter replace dev <iface> protocol ip parent 1:0 prio 5 u32 match ip sport <port> 0xffff match ip dst <ip>/32 flowid <subClassId>`。
- 1 秒防抖更新：当高频收到 IP 增删事件时，放入队列异步合并处理。

- [ ] **Step 4: 运行测试验证通过**

Run: `go test -v ./internal/trafficshaper -run TestReconcilerInboundAndClientSync`
Expected: PASS

- [ ] **Step 5: 提交更改**

```bash
git add internal/trafficshaper/reconciler.go internal/trafficshaper/reconciler_test.go
git commit -m "feat(trafficshaper): implement reconciler and dynamic IP syncing"
```

---

### Task 4: 业务层集成与生命周期守护 (Service & Job Integration)

**Files:**
- Modify: `internal/web/service/inbound.go`
- Modify: `internal/web/job/check_client_ip_job.go`
- Modify: `main.go`
- Test: `internal/web/service/inbound_rate_limit_test.go`

**Interfaces:**
- Consumes: `InboundService`, `CheckClientIpJob`, `trafficshaper.Reconciler`
- Produces: 自动化入站增删改流控联动，`x-ui tc clean` CLI 指令

- [ ] **Step 1: 编写业务集成单元测试**

```go
package service

import (
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestInboundServiceRateLimitIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_svc_rl.db")
	_ = database.InitDB(dbPath)
	defer func() { _ = database.CloseDB() }()

	svc := &InboundService{}
	inbound := &model.Inbound{
		Remark:           "svc-test-rl",
		Port:             28443,
		Protocol:         model.VLESS,
		InboundDownLimit: 50,
		ClientDownLimit:  5,
		Tag:              "in-svc-rl",
		Enable:           true,
	}

	err := svc.AddInbound(inbound)
	if err != nil {
		t.Fatalf("AddInbound failed: %v", err)
	}

	got, err := svc.GetInbound(inbound.Id)
	if err != nil || got == nil {
		t.Fatalf("GetInbound failed: %v", err)
	}
	if got.InboundDownLimit != 50 || got.ClientDownLimit != 5 {
		t.Errorf("Rate limit not stored correctly in service: %+v", got)
	}
}
```

- [ ] **Step 2: 运行测试验证**

Run: `go test -v ./internal/web/service -run TestInboundServiceRateLimitIntegration`
Expected: PASS (once data model fields are mapped)

- [ ] **Step 3: 业务层与定时任务打通**

- 在 `internal/web/job/check_client_ip_job.go`：在提取到活跃客户端 IP（`observed`）时，调用 `trafficshaper.GetReconciler().SyncAllObserved(observed)`。
- 在 `internal/web/service/inbound.go`：在 `AddInbound`、`UpdateInbound`、`DelInbound` 时通知 `trafficshaper` 更新入站限速规格。
- 在 `main.go`：在启动时调用 `trafficshaper.Init()`，注册停机钩子 `trafficshaper.Teardown()`，并添加 `x-ui tc clean` 指令。

- [ ] **Step 4: 运行所有相关 Go 测试**

Run: `make test-go`
Expected: PASS

- [ ] **Step 5: 提交更改**

```bash
git add internal/web/service/inbound.go internal/web/job/check_client_ip_job.go main.go internal/web/service/inbound_rate_limit_test.go
git commit -m "feat(web): integrate trafficshaper with inbound service and ip job"
```

---

### Task 5: 前端表单契约与代码生成 (Frontend Schema & OpenAPI Gen)

**Files:**
- Modify: `frontend/src/schemas/forms/inbound-form.ts`
- Modify: `frontend/src/models/dbinbound.ts`
- Test: `internal/web/routes_contract_test.go`

**Interfaces:**
- Consumes: Go Inbound model
- Produces: `frontend/src/generated/` types, OpenAPI schema

- [ ] **Step 1: 更新前端 Zod 与 Model 契约**

在 `frontend/src/schemas/forms/inbound-form.ts` 的 `InboundDbFieldsSchema` 中添加：
```ts
inboundDownLimit: z.number().int().min(0).default(0),
clientDownLimit: z.number().int().min(0).default(0),
```

在 `frontend/src/models/dbinbound.ts` 中添加对应类型字段。

- [ ] **Step 2: 运行代码生成工具**

Run: `make gen` (或者 `cd tools/openapigen && go run . && cd ../../frontend && npm run gen:openapi`)
Expected: 成功生成 `frontend/src/generated/` 代码与 `frontend/public/openapi.json`。

- [ ] **Step 3: 验证路由契约与 API 定义无漂移**

Run: `go test -v ./internal/web -run TestRouteRegistryContract`
Expected: PASS

- [ ] **Step 4: 提交生成的代码与契约文件**

```bash
git add frontend/src/schemas/forms/inbound-form.ts frontend/src/models/dbinbound.ts frontend/src/generated/ frontend/public/openapi.json
git commit -m "feat(api): update frontend schemas and generated openapi specs"
```

---

### Task 6: 前端 UI 交互呈现与完整国际化 (UI & i18n)

**Files:**
- Modify: `frontend/src/pages/inbounds/form/InboundFormModal.tsx`
- Modify: `frontend/src/pages/inbounds/list/InboundList.tsx`
- Modify: `internal/web/translation/*.json` (所有 13 个 locale 文件)
- Test: `frontend/src/test/i18n-dead-keys.test.ts`

**Interfaces:**
- Consumes: `InboundFormValues`
- Produces: 入站限速输入控件、列表徽标展示

- [ ] **Step 1: 在入站编辑弹窗中增加限速设置**

在 `frontend/src/pages/inbounds/form/InboundFormModal.tsx` 的基础配置区域，添加「带宽限速 (Mbps)」卡片，包含 `inboundDownLimit` 和 `clientDownLimit` 两个带 Tooltip 的 `InputNumber` 输入框。

- [ ] **Step 2: 在入站列表中展示限速徽标**

在 `frontend/src/pages/inbounds/list/InboundList.tsx` 中，如果 `inboundDownLimit > 0` 或 `clientDownLimit > 0`，渲染带有 `⚡` 符号的限速 Tag（例如 `⚡ 100M / 10M`）。

- [ ] **Step 3: 补全 13 个语言包中的 i18n 词条**

在 `internal/web/translation/` 下的所有 13 个语言 JSON 文件中，添加并翻译：
- `pages.inbounds.form.inboundDownLimit`
- `pages.inbounds.form.inboundDownLimitHint`
- `pages.inbounds.form.clientDownLimit`
- `pages.inbounds.form.clientDownLimitHint`
- `pages.inbounds.list.rateLimit`

- [ ] **Step 4: 运行国际化无死键测试与前端单测**

Run: `cd frontend && pnpm test`
Expected: PASS (`i18n-dead-keys.test.ts` 通过)

- [ ] **Step 5: 提交更改**

```bash
git add frontend/src/pages/inbounds/ internal/web/translation/
git commit -m "feat(frontend): add rate limiting inputs, badge and i18n"
```

---

### Task 7: 综合门禁验证与二开特性登记 (Verification & FEATURE.md)

**Files:**
- Modify: `FEATURE.md`
- Verification: `make verify`

- [ ] **Step 1: 登记 FEATURE.md 说明**

按 `AGENTS.md` 规范，在 `FEATURE.md` 中增加「入站节点与多客户端内核级下行限速」章节：
- 价值与场景：防止单个用户或单个节点突发流量跑满服务器出口带宽，避免高并发下导致服务降级或公网拥塞。
- 功能描述：基于 Linux `tc` HTB 树状层级令牌桶与在线客户端 IP 动态对齐，零 CPU 损耗，支持整入站限速与每客户端隔离限速，完全兼容上游官方 Xray 核心任意版本切换。

- [ ] **Step 2: 执行全量本地验证门禁**

Run: `make verify`
Expected: `gen-check`, `lint`, `typecheck`, `test`, `build` 全部 PASS。

- [ ] **Step 3: 提交最终文档**

```bash
git add FEATURE.md
git commit -m "docs(feature): record inbound rate limiting in FEATURE.md"
```
