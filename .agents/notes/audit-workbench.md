# Workbench integration

Audit was missing from shared CSS selectors and omitted dark/ultra root classes, which caused the visual mismatch. Extend the existing styles rather than create a new theme. Drop reserved api inbound control-plane events at capture, ingestion and query boundaries; pruning clears old records. Keep legitimate client loopback traffic and all existing client metrics. Remove project sponsor UI routes, dashboard/login/sidebar placements, and the public sponsor API. MTProto sponsored-channel tags remain separate.


## 入站备注仅在前端展示

审计明细通过共享 useInboundOptions 获取轻量入站列表，保留原始日志标签，并在渲染时追加当前备注。Map 按节点编号分组，子节点标签只剥离对应的 n{id}- 前缀一次，避免跨节点同名标签串备注；本机标签不剥离前缀。useMemo 仅在入站列表引用变化时构建，分页和 15 秒审计轮询只查 Map。请求在非子节点的明细模式且存在记录时启用，复用既有查询键与缓存失效；手动刷新同时刷新备注。

不修改日志表、采集/入库路径或审计响应。入站已删除、备注为空、列表尚未加载/请求失败时保留标签，不阻塞审计结果。回归覆盖节点隔离、列表延迟、分页不重复请求及失败降级。

## 完整网站流量排行

首页仍只显示前 20 个，标题右侧「更多」打开独立弹窗。弹窗挂载后才调用 audit/sites，所有时间/日期/节点/客户端/网站筛选直接来自主页面已应用状态。每页 20/50/100，默认 20，分页在服务器执行；筛选变化时派生第一页，避免 effect 重置导致先发送旧页码请求，已选页大小保留。主页面重新查询期间不卸载弹窗，防止页大小意外重置。

后端复用 auditMetricsQuery 与只读一致性快照，只查询网站分组数量和当前页，不生成客户端排行/趋势/访问明细。首页与完整排行共用 auditSiteRanking，流量并列时按网站名称稳定排序。参数限制每页最多 100 并检查偏移溢出。无新增日志持久化字段；已有保留期、主子节点模式和控制流量过滤不变。

验收覆盖超过 20 个网站的全部分页、20/50/100、稳定并列排序、首页与弹窗排行一致、所有筛选及历史汇总、错误/空结果、关闭时不请求、请求参数与真实页码、筛选重置第一页并保留每页数量。分页控件独立于表格，因此空结果也可选择数量。
