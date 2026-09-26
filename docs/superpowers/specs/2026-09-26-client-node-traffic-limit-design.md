# 客户端单节点流量上限与用量监控设计文档 (Spec)

## 1. 概述与核心价值 (Overview)

### 1.1 业务场景
在多节点代理部署场景下，不同节点往往具备差异化的线路成本（例如优质香港专线、原生家宽节点 vs 普通直连节点）。目前客户端仅支持全局总流量上限（`totalGB`），无法限制用户在特定节点上的使用额度。

### 1.2 核心目标
1. **单节点独立流量上限**：支持为客户端绑定的关联入站（节点）配置独立的流量上限（如客户端全局限额 200GB，其中节点 B 单独限额 50GB）。
2. **单节点用尽阻断（熔断）**：当客户端在节点 B 用满 50GB 后，仅熔断阻断节点 B 的连接，节点 A 依然正常可用，总剩余配额为 150GB。
3. **订阅链接保留与订阅展示页进度条呈现**：订阅链接接口（`getInboundsBySubId`）不过滤该节点（由服务端 Xray 运行时强行阻断连接）；在订阅信息展示网页（`/subscribe/:subId`）中，若存在设置了独立流量上限的节点，以进度条形式展示该节点的已用量与总配额。
4. **客户端编辑新增「流量」选项卡**：在 `/panel/clients` 客户端编辑弹窗中新增「流量」Tab，集中展示与配置全局总流量及各关联节点的流量上限与已用/剩余明细。

---

## 2. 数据模型与数据库迁移 (Data Models)

### 2.1 关联表字段扩展 (`model.ClientInbound`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 中，为 `ClientInbound` 增加 3 个数值字段：

```go
type ClientInbound struct {
	ClientId     int    `json:"clientId" gorm:"primaryKey;column:client_id;index"`
	InboundId    int    `json:"inboundId" gorm:"primaryKey;column:inbound_id;index"`
	FlowOverride string `json:"flowOverride" gorm:"column:flow_override"`
	DownLimit    int    `json:"downLimit" form:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0" example:"50"`
	// TotalGB 为该入站单独的流量上限（字节，0 表示无单独限制、仅受总流量限制）
	TotalGB      int64  `json:"totalGB" form:"totalGB" gorm:"column:total_gb;default:0" validate:"omitempty,gte=0" example:"53687091200"`
	// Up 与 Down 为该入站累计消耗的上下行流量（字节）
	Up           int64  `json:"up" form:"up" gorm:"column:up;default:0"`
	Down         int64  `json:"down" form:"down" gorm:"column:down;default:0"`
	CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}
```

### 2.2 传输模型扩展 (`model.Client`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 的 `Client` 结构中扩展传输字段：

```go
type Client struct {
	...
	// TotalGBByInbound 映射 inboundId -> 该入站流量上限（字节）
	TotalGBByInbound map[int]int64 `json:"totalGBByInbound,omitempty"`
	// InboundTraffics 映射 inboundId -> 该入站的用量统计详情
	InboundTraffics map[int]ClientInboundTraffic `json:"inboundTraffics,omitempty"`
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

### 2.3 数据库迁移 (`internal/database/db.go`)
在 `InitDB` 的迁移流程中增加：
- 检查并添加 `client_inbounds` 的 `total_gb`, `up`, `down` 列；
- 建立索引 `idx_client_inbounds_total_gb`（便于熔断扫描只针对 `total_gb > 0` 的行快速过滤）。

---

## 3. 业务服务层与调度记账 (Service Layer)

### 3.1 流量双向记账
在现有的 5 秒定时轮询中，无论是本地入站还是从节点上报流量：
1. **本地入站**（[`inbound_traffic.go`](file:///Users/ryan/Code/Go/3x-ui/internal/web/service/inbound_traffic.go) 的 `addClientTraffic`）：
   - 在已有的同一数据库写事务中，更新 `client_traffics` 累加全局流量的同时，依据 `t.Email` 和 `ibId` 定位 `client_id`：
   ```sql
   UPDATE client_inbounds SET up = up + ?, down = down + ? WHERE client_id = ? AND inbound_id = ?
   ```
2. **远程从节点**（[`inbound_node.go`](file:///Users/ryan/Code/Go/3x-ui/internal/web/service/inbound_node.go) 的 `setRemoteTrafficLocked`）：
   - 提取增量 `deltaUp, deltaDown`，同步更新对应的 `client_inbounds` 计数器。

### 3.2 单节点用尽阻断（熔断机制）
在 [`inbound_disable.go`](file:///Users/ryan/Code/Go/3x-ui/internal/web/service/inbound_disable.go) 的客户端失效检测循环中：
1. **单节点超额扫描**：
   ```sql
   SELECT client_id, inbound_id, total_gb, (up + down) AS used
   FROM client_inbounds
   WHERE total_gb > 0 AND (up + down) >= total_gb
   ```
2. **入站运行时阻断**：
   - 针对超额的 `(client_id, inbound_id)`，获取客户端 email，在目标 inbound 的 `settings["clients"]` 中将该客户端的 `enable` 置为 `false`；
   - 本地节点：执行 `trafficRemoveUser`，通过 Xray gRPC API 动态注销该用户在目标入站上的凭证；
   - 远程从节点：构建 `trafficInboundUpdatePlan` 推送配置至从节点对齐；
   - **隔离原则**：客户端全局状态 `clients.enable` 不受影响，其他未超额的入站依然处于可用状态。

### 3.3 周期重置与手动重置
1. **自动周期重置**（`autoRenewClients`）：当客户端触发周期重置时，同步执行 `UPDATE client_inbounds SET up = 0, down = 0 WHERE client_id = ?`。
2. **管理员手动重置**（`ResetClientTraffic`）：同步将关联入站的 `up` 与 `down` 归零。
3. **入站凭证恢复**：用量清零后（`used < total_gb`），如果客户端之前处于阻断状态，联动在入站 `settings["clients"]` 中将该客户端恢复为 `enable: true`，并通过 `trafficAddUser` 重新注册进 Xray 内存。

---

## 4. 订阅服务与展示页面 (Subscription)

### 4.1 订阅链接保留 (`internal/sub/service.go`)
- 按照用户明确要求，`getInboundsBySubId` 依然返回所有关联入站的代理节点配置（不剔除限流超额节点），由 Xray 服务端握手时拒绝连接。

### 4.2 订阅信息网页 (`SubPage.tsx` & `SubHero.tsx`)
1. **数据模型扩展 (`PageData`)**：
   在 [`service.go`](file:///Users/ryan/Code/Go/3x-ui/internal/sub/service.go) 的 `PageData` 中增加 `LimitedNodes` 列表：
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
2. **页面呈现**：
   - 在 [`SubPage.tsx`](file:///Users/ryan/Code/Go/3x-ui/frontend/src/pages/sub/SubPage.tsx) 主 Hero 概览下方，当 `limitedNodes` 存在时渲染独立卡片「节点流量配额 (Node Quotas)」；
   - 使用 Ant Design `<Progress>` 进度条展示每个受限节点的已用与总计；
   - 超过上限时进度条显示红色 warning/exception 状态，并打上 `已用尽 (Depleted)` 标签。

---

## 5. 客户端表单与交互设计 (Frontend UI)

在 [`ClientFormModal.tsx`](file:///Users/ryan/Code/Go/3x-ui/frontend/src/pages/clients/ClientFormModal.tsx) 中：
1. **Tab 结构优化**：
   - 基础信息 Tab 保留邮箱、启用、关联入站选择等基础信息；
   - 新增独立的「流量 (Traffic)」Tab。
2. **「流量」Tab 内容结构**：
   - **顶部卡片**：全局总流量配置输入框（`totalGB`，GB），编辑模式下展示全局已用/剩余状态，及自动重置周期（`trafficReset`、`trafficResetDay`）。
   - **下方表格**：关联入站流量配置与监控：
     - **节点 / 备注**：入站备注及所属节点标签
     - **协议 / 端口**：协议类型与端口
     - **已用流量**：如 `12.50 GB`
     - **节点流量上限 (GB)**：数字输入框，`min={0}`，`placeholder="跟随总配额"`
     - **剩余流量**：如 `37.50 GB`
     - **状态**：`正常` / `已用尽`

---

## 6. 验证与测试方案 (Testing Plan)

1. **单元测试 (`internal/web/service`)**：
   - `TestClientInboundTraffic_Accounting`：测试双向记账逻辑，验证流量增量同时准确累加全局表与关联入站表。
   - `TestClientInboundTraffic_Depletion`：测试单节点超额熔断逻辑，验证当且仅当超额节点移除用户，未超额节点不受影响。
   - `TestClientInboundTraffic_Reset`：测试手动重置与定时周期重置时关联表流量归零与节点自动恢复启用。
2. **订阅页面渲染测试 (`frontend/src/test`)**：
   - 验证 `SubPage` 在包含 `limitedNodes` 时的进度条渲染与正常/超额状态切换。
3. **全流程端到端验证**：
   - 在本地或 OrbStack VM 中配置客户端关联 A、B 两个节点（总限流 200G，B 节点限流 50G）；
   - 对 B 节点打流超额，验证 B 节点拒绝连接、A 节点正常通信；
   - 检查订阅信息页面进度条显示用尽状态。
