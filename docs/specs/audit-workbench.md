# Audit workbench consistency and control-plane exclusion

Use the shared page shell/card styles and light/dark/ultra theme classes used by client/inbound pages. Remove project sponsor placements from the dashboard, login page, desktop/mobile navigation, page routes, and public API. MTProto sponsored-channel ad tags remain a separate protocol feature.

The reserved `api` inbound is the panel control plane, not client browsing traffic. Consume its access lines without spooling, reject its legacy/node events at ingestion, exclude already-stored rows from every summary/detail query, and delete them during normal pruning. Do not exclude loopback targets from genuine client inbounds.

Acceptance: api events never change connection/byte/site counts, including old database rows; real client loopback traffic remains visible. No project sponsor UI route, placement, or public API remains. Audit shares other pages' padding, backgrounds and card styles on desktop/mobile and light/dark/ultra themes.

## 入站备注的前端映射展示

审计明细保留原始入站标签，在标签后追加当前入站备注，例如 `in-42342-tcp（香港入口）`。备注仅用于前端展示，不写入审计日志表，也不在审计接口中逐条查入站。

前端复用轻量入站列表 `/panel/api/inbounds/options` 及其共享查询缓存，仅在展示非子节点模式的连接明细且存在记录时请求。列表包含标签、备注和节点编号；列表数据变化时用 useMemo 一次性构建按节点编号分组的标签→备注 Map，每条日志只做 Map 查询。子节点列表标签中的控制端 `n{id}-` 前缀按对应节点还原一次，本机标签保持原样，禁止跨节点回退匹配。

日志分页、筛选和定时轮询不逐条请求、不重复构建映射。手动刷新可一并刷新列表，已有入站变更沿用共享缓存失效机制。入站已删除、备注为空或列表请求失败时，日志仍显示原标签；列表尚未加载时也不阻塞日志展示。展示当前备注，不追溯保存历史备注。

验收：本机与两个子节点相同原生标签分别匹配各自备注；空备注/已删除入站保留标签；多条记录与分页共用一次列表请求，显示过程中不发起单条入站查询。
