# 客户端单节点独立流量上限与用量监控 - OrbStack 真机模拟测试报告

## 1. 测试环境与概述

本测试在真实 OrbStack 虚拟机环境 (`orb -m ubuntu`) 中执行，直接拉起实际 Xray 核心服务端进程、Xray 客户端代理矩阵、真实目标数据源服务以及 3x-ui 服务层和 SQLite 数据库，全真机模拟多客户端在多入站场景下的流量记账、单节点熔断断流、多客户端隔离、订阅页面进度条注入、重置自愈以及全局总配额全阻断的全流程验证。

- **操作系统**: Ubuntu 24.04 LTS on OrbStack Linux Kernel 7.0.14-orbstack (aarch64)
- **Go 运行时**: go1.27.1 linux/arm64
- **Xray 核心**: Xray 26.9.9 (Xray, Penetrates Everything.) Custom
- **测试工具脚本**:
  - `test/benchmark/benchmark_node_traffic_limit.sh`
  - `test/benchmark/simtest_traffic_limit.go`

---

## 2. 模拟拓扑与客户端配置

### 2.1 服务端与入站节点 (Server Inbounds)
- **Node A (普通直连节点)**:
  - 端口: `41001`, 协议: `VLESS`, 标签: `node-a-direct`, 备注: `Node-A-Direct`
- **Node B (昂贵 VIP/专线节点)**:
  - 端口: `41002`, 协议: `VLESS`, 标签: `node-b-vip`, 备注: `Node-B-VIP`
- **API Inbound (Xray gRPC 管理接口)**:
  - 端口: `62789`, 运行 `HandlerService`, `StatsService`

### 2.2 客户端矩阵 (Clients)
| 客户端 | 邮箱 | UUID | 全局总配额 | Node A 限额 | Node B 限额 | 角色与预期定位 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Client 1** | `client1@test.com` | `11111111-...` | 100 MB | 0 (无单独上限) | **20 MB** | 核心受限测试客户端 |
| **Client 2** | `client2@test.com` | `22222222-...` | 100 MB | 0 (无单独上限) | **50 MB** | 伴随客户端 (高配额，验证互不干扰) |
| **Client 3** | `client3@test.com` | `33333333-...` | 100 MB | 0 (无单独上限) | 0 (无单独上限) | 伴随客户端 (无上限，验证互不干扰) |

### 2.3 客户端代理出口 (Client SOCKS5 Proxies)
- **Proxy 1 (`c1_on_a`)**: 监听 `127.0.0.1:10801`，通过 VLESS 经由 Node A 发送请求（Client 1 凭据）。
- **Proxy 2 (`c1_on_b`)**: 监听 `127.0.0.1:10802`，通过 VLESS 经由 Node B 发送请求（Client 1 凭据）。
- **Proxy 3 (`c2_on_b`)**: 监听 `127.0.0.1:10803`，通过 VLESS 经由 Node B 发送请求（Client 2 凭据）。
- **Proxy 4 (`c3_on_b`)**: 监听 `127.0.0.1:10804`，通过 VLESS 经由 Node B 发送请求（Client 3 凭据）。

---

## 3. 全场景真机模拟测试执行与验证

### 场景 1: 基准连通性验证 (Baseline Connectivity)
- **操作**: 4 个客户端代理分别向目标服务 `http://127.0.0.1:18080/health` 发送 GET 请求。
- **实测结果**:
  - `c1_on_a` (Client 1 on Node A): **HTTP 200 OK**
  - `c1_on_b` (Client 1 on Node B): **HTTP 200 OK**
  - `c2_on_b` (Client 2 on Node B): **HTTP 200 OK**
  - `c3_on_b` (Client 3 on Node B): **HTTP 200 OK**
- **判定**: **PASS**（全部 4 条代理链路初始状态 100% 畅通）。

### 场景 2: 真实流量产生与双向原子记账验证 (Dual-Accounting)
- **操作**: Client 1 经由 Proxy 2 (`c1_on_b`) 向目标服务下载 8 MB 真实二进制流并上报。
- **数据库断言**:
  - `client_inbounds (client=1, inbound=B)`: `up + down = 8.00 MB`
  - `client_inbounds (client=1, inbound=A)`: `up + down = 0 B` (严格隔离，未走 A 流量不增加)
  - `client_traffics (email=client1@test.com)`: 全局总用量 `up + down = 8.00 MB`
  - `client_records.enable`: `true`
- **网络层断言**:
  - Client 1 再次访问 Node B: **HTTP 200 OK**（8 MB < 20 MB，链路依然活跃）。
- **判定**: **PASS**（双向精准原子记账完全符合规范）。

### 场景 3: 单节点超额精准熔断与节点间隔离 (Single-Node Depletion & Cutoff)
- **操作**: Client 1 经由 Proxy 2 (`c1_on_b`) 再次下载 15 MB 真实二进制流（Node B 累计用量达到 23 MB，超过该节点设定的 20 MB 配额上限），触发 `InboundService.AddTraffic` 流量周期与客户端生命周期熔断检查。
- **底层与数据库状态**:
  - `client_inbounds (client=1, inbound=B)` 累计用量 23.00 MB >= 20.00 MB。
  - Inbound B `settings.clients`: Client 1 状态被更新为 `enable: false`。
  - Inbound A `settings.clients`: Client 1 状态依然保持 `enable: true`。
  - 全局表 `client_records`: `enable` 依然保持 `true`（总用量 23 MB 远未达到全局 100 MB 配额）。
  - Xray gRPC 实时注销: Xray 核心动态从 Node B 中卸载 Client 1 凭据。
- **真实网络访问断言**:
  1. **Client 1 on Node B (`c1_on_b`)**: **被拒绝/阻断 (BLOCKED)**。
     - Xray 核心抛出原生错误: `rejected proxy/vless/encoding: invalid request user id: 11111111-1111-1111-1111-111111111111`，curl 退出码 52。
  2. **Client 1 on Node A (`c1_on_a`)**: **HTTP 200 OK**（畅通无阻，流量与连接未受任何影响）。
  3. **Client 2 on Node B (`c2_on_b`)**: **HTTP 200 OK**（配额 50 MB，未超额，完全不受影响）。
  4. **Client 3 on Node B (`c3_on_b`)**: **HTTP 200 OK**（无配额限制，完全不受影响）。
- **判定**: **PASS**（单节点熔断、多节点隔离、多客户端相互隔离 100% 达成）。

### 场景 4: 订阅服务与展示页进度条数据注入验证 (Subscription Page Data Injection)
- **操作**: 针对超额的 Client 1 调用 `SubService.BuildPageData` 生成订阅网页上下文。
- **数据结构断言**:
  - `pageData.LimitedNodes` 数组长度恰为 1（仅展示有限额配置的节点，未限额的 Node A 不展示）。
  - `node.Name`: `"Node-B-VIP"`
  - `node.Total`: `"20.00MB"`
  - `node.Used`: `"23.00MB"`
  - `node.Remained`: `"0.00B"`
  - `node.Depleted`: `true`
  - `node.Percent`: `115.0%`
- **判定**: **PASS**（为前端 Ant Design `<Progress>` 进度条提供精确计算的数据，完美呈现用尽状态）。

### 场景 5: 管理员重置 / 自动周期轮转自愈验证 (Reset & Auto-Healing)
- **操作**: 管理员调用 `InboundService.ResetClientTraffic(ibB.Id, "client1@test.com")` 重置流量。
- **底层与数据库状态**:
  - `client_inbounds (client=1, inbound=B)` 用量同步清零: `up = 0, down = 0`。
  - Inbound B `settings.clients`: Client 1 自动恢复为 `enable: true`。
  - Xray gRPC 实时注册: `runtime.AddUser` 重新将 Client 1 加载进 Node B 内存。
- **真实网络访问断言**:
  - Client 1 访问 Node B (`c1_on_b`): **立即恢复 HTTP 200 OK**！
  - Client 1 访问 Node A (`c1_on_a`): **保持正常 HTTP 200 OK**！
- **判定**: **PASS**（重置后单节点自动复活自愈，无需重启 Xray 进程）。

### 场景 6: 全局总配额用尽全阻断对比验证 (Global Quota Depletion Contrast)
- **操作**: Client 1 经由 Node A 消耗 105 MB 流量，直接击穿全局 100 MB 总套餐配额。
- **底层与数据库状态**:
  - 全局表 `client_records`: `enable` 被置为 `false`。
  - 所有关联节点（Node A 与 Node B）均将 Client 1 注销。
- **真实网络访问断言**:
  - Client 1 访问 Node A (`c1_on_a`): **被拒绝/阻断 (BLOCKED)**（Xray 拒绝凭证）。
  - Client 1 访问 Node B (`c1_on_b`): **被拒绝/阻断 (BLOCKED)**（Xray 拒绝凭证）。
  - Client 2 访问 Node B (`c2_on_b`): **保持正常 HTTP 200 OK**（独立客户端依然正常）。
- **判定**: **PASS**（全局限流与单节点限流机制对比鲜明，判定边界完全正确）。

---

## 4. 结论

通过在 OrbStack 虚拟机环境中使用真实 Xray 核心、真实代理端口和真实网络数据包，验证了以下核心场景：
1. **基准运行正常**：多节点与多客户端代理链路建立顺畅。
2. **双向原子记账精准**：单入站独立统计与全局总用量累加严格一致且隔离。
3. **单节点独立熔断精准**：特定节点超额仅切断该节点的 Xray 运行时会话，不污染其他节点，不污染其他客户端，更不会过早切断客户的全局连接。
4. **订阅进度条数据完备**：受限节点信息、百分比、用尽标记准确注入。
5. **重置自愈无缝**：重置使用量后，Xray 内存即刻热重载生效，网络请求瞬间复活。
6. **全链路行为符合预期**：所有测试场景 100% 通过。
