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

## 最近一分钟速率与高频提示

首页第四张卡片改为「每秒连接 / 网站」。当前速率固定取服务器当前 UTC 时刻之前的滚动 60 秒：连接速率为有效连接日志数 / 60，网站速率为不同归一化网站数 / 60（同一网站重复连接不增加网站数）。只读取已收到的连接完成日志，仍在传输的连接与尚未同步的子节点日志不会提前计入。两项速率显示三位小数；零字节记录仍是有效连接，api 控制流量排除。

新增 GET /panel/api/audit/rates 返回窗口起止、当前连接/网站计数与高频客户端；新增 GET /panel/api/audit/rates/history 返回最近 24 小时的 1440 个完整 UTC 分钟桶及可用历史起点。各分钟连接数与不同网站数分别 / 60 得到图表的每秒速率，补齐已覆盖时间内的空分钟为零。两个接口均应用主页面已提交的节点、客户端、网站筛选，时间窗口独立于历史日期与总量窗口。客户端筛选同时区分图表；不将所有客户端数据逐条下载后筛选。当前数据每 15 秒刷新，24 小时图每 60 秒刷新，并支持手动刷新。

图表复用现有交互式折线图，展示连接/秒、网站/秒，悬停显示分钟和速率。选择单个客户端时显示启用的阈值参考线，全部客户端合计不使用单客户端阈值线。每批日志在入库事务中按分钟、节点、客户端、网站更新连接计数，仅保留最近 24 小时；使用 INSERT RETURNING 识别真正新入库的明细，迟到日志沿用批量回执去重，重复投递不会重复计数。历史查询只读分钟计数，不反复扫描 24 小时原始日志，也不逐客户端查询。明细压缩/过期不删除仍在 24 小时内的分钟计数。

数据库首次迁移在同一事务内从尚存明细批量生成分钟计数，并记录可用历史起点；后续启动跳过回填。升级前已经清理的分钟数据不能重建，界面明确标示可用历史起点，不将未知历史当作零。清空日志后以清理水位作为新的可用起点。

审计设置新增连接/秒速率阈值和网站/秒速率阈值，默认 10 与 1，允许 0..1000000 的有限数值，0 表示关闭该项。高频提示只比较同一滚动 60 秒内每个客户端的实际计数 / 60，任一启用阈值达到即提示，最多展示 20 个客户端；移除历史总数与累计 50 个网站触发条件。阈值持久化，保存后立即影响查询，并随版本化审计策略同步到子节点；旧 API 只提交留存字段时保留现有阈值。清理覆盖分钟汇总，清理水位之前的迟到日志不得重新创建它们。速率查询复用有界只读快照，不持有写锁、不逐客户端查询。

审计文案插值统一采用应用配置的单层花括号，例如 {hours}、{days}；修复留存提示及相关审计文案中误用双层花括号导致原样显示的问题。
