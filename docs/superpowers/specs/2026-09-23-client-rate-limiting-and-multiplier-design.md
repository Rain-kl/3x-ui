# 客户端差异化限速与入站流量倍率技术设计 (Design Spec)

## 1. 概述与需求背景

在 3x-ui 面板中，管理员需要为不同客户端配置差异化的网络服务质量（QoS）：
1. **客户端层级限速**：管理员在 `/panel/clients` 客户端管理中，不仅能配置客户端全局默认带宽（总限速），还能针对该客户端所挂载的不同入站（节点）单独配置覆盖限速（例如某用户默认 100Mbps，但在特定昂贵节点上限制为 50Mbps）。
2. **入站流量计费倍率**：不同入站/节点的物理线路成本各异（如普通 VPS vs 昂贵 IPLC 专线），管理员需要为入站设置流量扣费倍率（如 1.5x、2.0x），并在流量同步与客户端配额扣减时按此倍率加权折算。
3. **真实环境 5 客户端压测**：在 OrbStack Linux 虚拟机中搭建 5 个真实客户端并发测速场景，全面覆盖整站限速、用户全局限速、节点级覆盖限速与默认限速。

---

## 2. 数据模型与存储设计

### 2.1 入站模型扩展 (`model.Inbound`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 中：
```go
// Inbound represents an Xray inbound configuration with traffic and protocol settings.
type Inbound struct {
    ...
    // TrafficRatio sets the traffic billing multiplier for this inbound (default: 1.0).
    TrafficRatio float64 `json:"trafficRatio" form:"trafficRatio" gorm:"column:traffic_ratio;default:1.0" validate:"omitempty,gte=0"`
    ...
}
```

### 2.2 客户端总记录扩展 (`model.ClientRecord`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 中：
```go
type ClientRecord struct {
    ...
    // DownLimit sets the default client downlink bandwidth limit in Mbps (0 = unlimited / inherit inbound default).
    DownLimit int `json:"downLimit" form:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0"`
    ...
}
```

### 2.3 客户端-入站关联表扩展 (`model.ClientInbound`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 中：
```go
type ClientInbound struct {
    ClientId     int    `json:"clientId" gorm:"primaryKey;column:client_id;index"`
    InboundId    int    `json:"inboundId" gorm:"primaryKey;column:inbound_id;index"`
    FlowOverride string `json:"flowOverride" gorm:"column:flow_override"`
    // DownLimit sets the per-inbound override downlink limit in Mbps for this client (0 = use client default DownLimit).
    DownLimit    int    `json:"downLimit" gorm:"column:down_limit;default:0" validate:"omitempty,gte=0"`
    CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
}
```

### 2.4 入站内嵌客户端模型扩展 (`model.Client`)
在 [`internal/database/model/model.go`](file:///Users/ryan/Code/Go/3x-ui/internal/database/model/model.go) 中：
```go
type Client struct {
    ...
    // DownLimit is the effective downlink limit in Mbps for this client on this inbound.
    DownLimit int `json:"downLimit,omitempty"`
    // DownLimitByInbound optionally maps inboundId -> override limit in Mbps during API create/update.
    DownLimitByInbound map[int]int `json:"downLimitByInbound,omitempty"`
    ...
}
```

### 2.5 数据库迁移与平滑升级 (`internal/database/db.go`)
- 在 GORM `AutoMigrate` 中自动升级 `inbounds`（新增 `traffic_ratio`）、`clients`（新增 `down_limit`）、`client_inbounds`（新增 `down_limit`）。
- 对旧数据执行回填更新：未设置的 `traffic_ratio` 默认置为 `1.0`，未设置的 `down_limit` 置为 `0`。

---

## 3. 业务逻辑与流控调度

### 3.1 客户端有效限速判定与优先级规则
客户端在特定入站连接时，其最终生效的下行带宽按以下优先级决定：
```
有效限速 (Effective Down Limit) =
  1. IF client_inbounds.down_limit > 0 THEN client_inbounds.down_limit
  2. ELSE IF client.down_limit > 0 THEN client.down_limit
  3. ELSE IF inbound.client_down_limit > 0 THEN inbound.client_down_limit
  4. ELSE 0 (不限速，直接共享入站带宽)
```
当保存/更新客户端时，`ClientService` 负责计算出各关联入站上的有效 `DownLimit` 并同步写入该入站 `settings.clients` 的内嵌 `Client.DownLimit`。

### 3.2 Linux TC 流控调度扩展 (`trafficshaper.Reconciler`)
1. **数据结构**：
   `InboundRule` 扩展支持 `ClientLimits map[string]int`（记录每个 `email` 的有效限速 Mbps）。
2. **多客户端差异化 Class 动态挂载**：
   - 客户端上线并识别出有效限速 `limit > 0` 时，动态在入站 Class `1:<inboundMinor>` 下分配专属叶子 Class `1:<clientMinor>`：
     - `rate = fmt.Sprintf("%dmbit", limit)`
     - `ceil = fmt.Sprintf("%dmbit", min(limit, inboundDownLimit))`
     - `burst = "32k", cburst = "32k"`
   - 为该客户端的每个在线 IP 添加 `tc filter` 规则（优先级 `prio 5`），指向其专属 Class。
   - 客户端断开连接或限速变更为 0 时，自动注销 `tc filter` 与 leaf class，释放 minor 序号。
3. **入站总带宽约束**：
   所有客户端子类挂载在入站根类（`1:<inboundMinor>`）下，整站总吞吐严格不超过 `InboundDownLimit`。

### 3.3 流量计费倍率加权逻辑 (`inbound_traffic` & `node_traffic_sync_job`)
1. **本地 Xray 流量**：
   在 `InboundService.addClientTraffic` 中，解析客户端所属入站的 `TrafficRatio`。若 `ratio > 0 && ratio != 1.0`，则计费增量为：
   $$\Delta Up_{\text{billed}} = \lfloor \Delta Up \times \text{ratio} \rfloor$$
   $$\Delta Down_{\text{billed}} = \lfloor \Delta Down \times \text{ratio} \rfloor$$
2. **多节点集群从节点流量**：
   在 `NodeTrafficSyncJob.setRemoteTrafficLocked` 中，根据从节点上报的入站标签匹配到控制端对应的入站记录 `c`。增量流量按 `c.TrafficRatio` 乘算折算后再执行持久化写入和配额扣减。

---

## 4. 前端交互与 API 设计

### 4.1 客户端配置弹窗 (`ClientFormModal.tsx`)
- 在「基础配置」「凭据设置」「分享链接」旁新增**「限速」Tab**（`pages.clients.tabRateLimit`）。
- **默认下行限速（Mbps）**：数字输入框，0 为不限速。
- **关联节点限速覆盖表格**：
  - 动态列出当前选中的关联入站（节点）；
  - 每行显示：入站备注、协议/端口、所属节点标签（如 `[Worker 1] VLESS-443`）；
  - 提供单节点下行限速输入框（Mbps），留空或 0 显示占位提示“继承总限速 (xx Mbps)”。

### 4.2 客户端列表 (`ClientsPage.tsx`)
- 在客户端列表表格中，直观展示该客户端的限速配置状态（例如 `100 Mbps`，若有节点覆盖则展示 `100 Mbps (2 个覆盖)`）。

### 4.3 入站配置弹窗 (`InboundModal.tsx`)
- 在入站编辑弹窗的「限速」Tab 中增加「流量计费倍率」输入项，支持配置浮点数（步长 0.1，默认 1.0）。

### 4.4 OpenAPI 与 API 路由契约
- 确保 `Inbound` 与 `ClientRecord`、`ClientCreatePayload` 在 `tools/openapigen` 导出新字段，通过 `make gen` 同步到前端 Zod Schema。

---

## 5. 真实压测验证计划 (OrbStack 5 客户端)

在 OrbStack 虚拟机中构建真实测试环境：
1. **测试入站环境**：
   - 监听端口 `54321`，协议 `VLESS-Reality`；
   - 整站下行限速 `inboundDownLimit = 100` (100 Mbps)；
   - 默认客户端限速 `clientDownLimit = 10` (10 Mbps)。
2. **5 个测试客户端配置**：
   - `client1@test.com`：无独立限速，测试回落生效入站默认限速 **10 Mbps**；
   - `client2@test.com`：设置客户端全局限速 **20 Mbps**，测试生效 **20 Mbps**；
   - `client3@test.com`：设置客户端全局限速 **30 Mbps**，测试生效 **30 Mbps**；
   - `client4@test.com`：设置客户端全局限速 **50 Mbps**，但针对该测试节点单独覆盖限速为 **15 Mbps**，测试验证节点级覆盖生效 **15 Mbps**；
   - `client5@test.com`：设置客户端全局限速 **50 Mbps**，未覆盖该节点，测试生效全局限速 **50 Mbps**。
3. **并发压测与断言**：
   - 使用 `curl` / `iperf3` 通过真实代理并发拉取测试大文件；
   - 验证各客户端实测带宽与预期严格吻合，误差不超过 $\pm 10\%$；
   - 验证 5 客户端并发拉流时，总聚合流量不超出入站总限速（100 Mbps）。
