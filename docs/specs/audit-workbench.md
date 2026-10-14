# Audit workbench consistency and control-plane exclusion

Use the shared page shell/card styles and light/dark/ultra theme classes used by client/inbound pages. Remove project sponsor placements from the dashboard, login page, desktop/mobile navigation, page routes, and public API. MTProto sponsored-channel ad tags remain a separate protocol feature.

The reserved `api` inbound is the panel control plane, not client browsing traffic. Consume its access lines without spooling, reject its legacy/node events at ingestion, exclude already-stored rows from every summary/detail query, and delete them during normal pruning. Do not exclude loopback targets from genuine client inbounds.

Acceptance: api events never change connection/byte/site counts, including old database rows; real client loopback traffic remains visible. No project sponsor UI route, placement, or public API remains. Audit shares other pages' padding, backgrounds and card styles on desktop/mobile and light/dark/ultra themes.

## 入站备注的前端映射展示

审计明细保留原始入站标签，在标签后追加当前入站备注，例如 `in-42342-tcp（香港入口）`。备注仅用于前端展示，不写入审计日志表，也不在审计接口中逐条查入站。

前端复用轻量入站列表 `/panel/api/inbounds/options` 及其共享查询缓存，仅在展示非子节点模式的连接明细且存在记录时请求。列表包含标签、备注和节点编号；列表数据变化时用 useMemo 一次性构建按节点编号分组的标签→备注 Map，每条日志只做 Map 查询。子节点列表标签中的控制端 `n{id}-` 前缀按对应节点还原一次，本机标签保持原样，禁止跨节点回退匹配。

日志分页、筛选和定时轮询不逐条请求、不重复构建映射。手动刷新可一并刷新列表，已有入站变更沿用共享缓存失效机制。入站已删除、备注为空或列表请求失败时，日志仍显示原标签；列表尚未加载时也不阻塞日志展示。展示当前备注，不追溯保存历史备注。

验收：本机与两个子节点相同原生标签分别匹配各自备注；空备注/已删除入站保留标签；多条记录与分页共用一次列表请求，显示过程中不发起单条入站查询。

## 网站流量排行分页弹窗

首页保留前 20 个网站的摘要，在标题右侧添加可点击的「更多」。弹窗按主页面当前已应用的时间窗口、UTC 日期、节点、客户端和网站筛选查询完整排行；不使用首页前 20 个结果做前端假分页。

新增 GET /panel/api/audit/sites，接收与 summary 相同的 hours/day/nodeId/client/site 筛选及 page/pageSize。默认 page=1、pageSize=20，仅接受每页 20/50/100 条，页码必须为正整数且偏移不得溢出。返回 items、total（不同网站数量）、page、pageSize、centralized。按上传+下载总字节降序、网站名升序排序，同字节值时顺序稳定；排行与首页复用同一聚合查询。明细与历史汇总、留存边界、api 排除、未知字节及子节点模式行为沿用审计查询。

后端在现有有界读快照中执行分组数量和当前页查询，只返回当前页；不查询无关客户端、小时趋势和连接明细，不逐网站执行查询，不持有审计写锁。前端仅在弹窗打开后请求，切换页码或 20/50/100 时向后端查询；改变每页条数及主页面筛选时回到第一页。弹窗内筛选始终跟随主页面已应用值，每页数量在筛选改变时保留，重新打开默认 20。错误允许重试，加载和空结果可见。

验收：超过 20 个网站可完整分页，20/50/100 正确生效；同流量网站跨页不重复遗漏；节点/客户端/网站/小时/UTC 日期与首页一致，包含历史汇总；弹窗关闭时无排行分页请求，筛选或页大小改变不会发送旧页码的查询。
