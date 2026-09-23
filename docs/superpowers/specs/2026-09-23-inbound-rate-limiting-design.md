# 入站与客户端带宽限速设计规范 (Inbound & Client Rate Limiting Design Spec)

## 1. 概述与背景 (Overview & Motivation)
在代理服务与多节点分布式管理中，入站带宽滥用（如单个客户端独占高带宽下载或整个节点出口拥塞）是常见痛点。当前 3x-ui 面板原生支持总流量额度（Quota）与 IP 数量限制（`limitIp`），但尚未提供平滑的速率控制（Bandwidth Shaping / Rate Limiting）。

本功能旨在为 3x-ui 提供高精度、高性能损耗极低、架构高内聚且完全解耦于 Xray-core 核心的带宽限速方案：
- **针对特定入站节点限速**：可配置入站总下行出口带宽上限。
- **可配置适用范围**：支持为入站配置“整入站限速”以及“每客户端独立限速”。
- **性能与解耦优先**：基于 Linux 内核级流量控制（`tc` + HTB 分层令牌桶），数据转发由内核调度，零 Go GC 压力，零内存拷贝开销，且完全不修改 Xray-core 源码，不破坏面板自由切换与锁定官方 Xray 版本的核心能力。

---

## 2. 设计目标与边界 (Goals & Non-Goals)

### 2.1 目标 (Goals)
1. **入站总限速 (Inbound-Level Downlink Limit)**：
   - 限制指定入站节点对外发往客户端的最大下行带宽（Mbps）。
2. **每客户端限速 (Per-Client Downlink Limit)**：
   - 限制该入站下每个活跃客户端的最大下行带宽（Mbps），不同客户端互不挤占。
3. **层级复合调度 (Hierarchical Token Bucket)**：
   - 支持同时设置入站总限速与每客户端限速。各客户端独立受控，且所有客户端瞬时总和不超过入站总上限。
4. **动态 IP 联动与增量对齐 (Dynamic IP Reconcile)**：
   - 复用现有在线 IP 探测通道，客户端连接或断开时动态维护内核过滤器，自动防抖批量聚合更新。
5. **高可用与宕机自愈 (High Availability & Self-Healing)**：
   - 保证 SSH/管理端口绝不被误限速；整机重启零残留；面板进程异常退出不影响系统网络，重启后自动抹除残留并对齐最新配置。
6. **优雅降级 (Graceful Degradation)**：
   - 在 macOS/Windows 本地开发环境或无 `CAP_NET_ADMIN` 特权的容器环境中自动转为旁路模式，友好提示并不阻塞系统启动。

### 2.2 非目标 (Non-Goals)
- 本阶段不针对客户端上传（Ingress，客户端往 VPS 上行）做限速，避免引入 `ifb` 虚拟设备和复杂重定向。
- 不采用二开定制 Xray-core 的方案，保证与官方上游二进制 100% 解耦。

---

## 3. 系统架构与分层设计 (Architecture & Layering)

```
+-------------------------------------------------------------------------+
|                              Web 前端 (React)                           |
|  - 入站编辑弹窗: 输入入站总下行限速 (Mbps) 与 每客户端限速 (Mbps)           |
|  - 节点列表展示: 渲染限速徽标 (Badge)                                     |
+------------------------------------+------------------------------------+
                                     | HTTP REST API
+------------------------------------v------------------------------------+
|                         3x-ui 面板核心 (Go 后端)                         |
|  1. Data Model & DB: Inbound 扩展 `inbound_down_limit`, `client_down_limit` |
|  2. CheckClientIpJob: 采集在线客户端的活跃 IP                             |
|  3. TrafficShaper Manager (internal/trafficshaper):                     |
|     - Engine: 状态机，探测默认网关设备 (eth0)，初始化根 HTB 队列              |
|     - Reconciler: 增量对比计算，1s 窗口防抖聚合                             |
|     - Executor: 执行系统 `tc` 指令 (带超时与并发保护)                       |
+------------------------------------+------------------------------------+
                                     | Linux 系统调用 (tc / netlink)
+------------------------------------v------------------------------------+
|                       Linux 内核网络层 (Kernel tc HTB)                   |
|  - 根队列: 1: htb default 9999 (未限速流量满速直通，保护 SSH 与其它节点)      |
|  - 限制根类: 1:1 (总限速池)                                              |
|    |-- 入站 A 类 1:10 (match sport 443, rate 100Mbps)                   |
|    |   |-- 客户端 1 类 1:101 (match dst IP1, rate 10Mbps) + fq_codel    |
|    |   +-- 客户端 2 类 1:102 (match dst IP2, rate 10Mbps) + fq_codel    |
|    +-- 入站 B 类 1:20 (match sport 8443, rate 50Mbps)                   |
+-------------------------------------------------------------------------+
```

---

## 4. 详细组件与流程设计

### 4.1 数据模型变更
#### `internal/database/model/model.go`
在 `Inbound` 模型增加以下字段：
```go
type Inbound struct {
    // ...
    // InboundDownLimit: 整个入站节点的总下行限速 (Mbps)，0 表示不限制
    InboundDownLimit int `json:"inboundDownLimit" form:"inboundDownLimit" gorm:"column:inbound_down_limit;default:0" validate:"omitempty,gte=0"`

    // ClientDownLimit: 该入站下每个客户端的下行限速 (Mbps)，0 表示不限制
    ClientDownLimit int `json:"clientDownLimit" form:"clientDownLimit" gorm:"column:client_down_limit;default:0" validate:"omitempty,gte=0"`
}
```

#### `internal/database/db.go`
增加版本迁移，使用 `ALTER TABLE inbounds ADD COLUMN ...` 保证已有数据库无缝升级。

### 4.2 内核流控管理器 (`internal/trafficshaper/`)
- **`engine.go` (引擎与初始化)**:
  - 自动定位外网主网卡名称（通过解析 `/proc/net/route` 或 `ip route show default`）。
  - 初始化根队列：
    ```bash
    tc qdisc replace dev <iface> root handle 1: htb default 9999
    tc class replace dev <iface> parent 1: classid 1:9999 htb rate 10gbit ceil 10gbit
    tc class replace dev <iface> parent 1: classid 1:1 htb rate 10gbit ceil 10gbit
    ```
  - 优雅停机时执行：
    ```bash
    tc qdisc del dev <iface> root 2>/dev/null || true
    ```
- **`reconciler.go` (增量对齐)**:
  - 维护内存期望状态：`map[inboundId]InboundRule`，包含 `Port`, `InboundDownLimit`, `ClientDownLimit` 及当前在线活跃的 `ClientIPs`。
  - 支持增量差异比对（Add, Update, Delete），避免全量刷新网卡。
  - 引入防抖机制：短时间内批量接收到的在线 IP 变动，在 1 秒定时器中聚合为一次更新。
- **`executor.go` (TC 命令行封装)**:
  - 封装对 `tc class`, `tc filter`, `tc qdisc` 的操作，添加 `context.WithTimeout(ctx, 3*time.Second)`。
  - 针对客户端叶子 Class 自动附加 `fq_codel` 调度算法，防止 Bufferbloat（缓冲膨胀引起的延迟升高）。

### 4.3 故障容错与自愈
1. **冷启动清理**：启动时主动探查并删除网卡上遗留的 `1:` 队列，确保干净初始化。
2. **直通保护**：未被过滤器捕获的流量全部导入 `1:9999` 类，满速直通，彻底避免 SSH 与其他服务受阻。
3. **CLI 运维兜底**：在 `x-ui` CLI 工具中增加 `x-ui tc clean` 指令，供用户随时应急一键清空网卡流控规则。

### 4.4 前端与国际化
- **表单输入**：在入站编辑弹窗添加 `inboundDownLimit` 和 `clientDownLimit` 数字输入框，单位 `Mbps`。
- **徽标呈现**：在入站列表卡片/行中，对启用了限速的入站展示醒目的 Badge。
- **国际化**：在 `internal/web/translation/` 13 个语言 JSON 文件中同步增补翻译键值。

---

## 5. 验证与测试计划 (Verification Plan)

1. **单元测试 (Go)**：
   - 针对 `trafficshaper` 的参数解析、Class ID 生成算法、增量对齐 Diff 逻辑进行单元测试。
   - 针对非 Linux 平台的降级逻辑进行验证。
2. **集成测试 (Linux 环境 / 模拟环境)**：
   - 验证 `tc` 树状结构指令的拼装合法性与执行幂等性。
   - 验证冷启动全量重置、更新入站限速值、客户端上线下线时的规则对齐。
3. **前端契约与构建验证**：
   - `make gen` 生成最新的 TypeScript 类型与 OpenAPI 定义。
   - `make verify` 通过前端类型检查、Lint 与测试。
