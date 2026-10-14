# Workbench integration

Audit was missing from shared CSS selectors and omitted dark/ultra root classes, which caused the visual mismatch. Extend the existing styles rather than create a new theme. Drop reserved api inbound control-plane events at capture, ingestion and query boundaries; pruning clears old records. Keep legitimate client loopback traffic and all existing client metrics. Remove project sponsor UI routes, dashboard/login/sidebar placements, and the public sponsor API. MTProto sponsored-channel tags remain separate.


## 入站备注仅在前端展示

审计明细通过共享 useInboundOptions 获取轻量入站列表，保留原始日志标签，并在渲染时追加当前备注。Map 按节点编号分组，子节点标签只剥离对应的 n{id}- 前缀一次，避免跨节点同名标签串备注；本机标签不剥离前缀。useMemo 仅在入站列表引用变化时构建，分页和 15 秒审计轮询只查 Map。请求在非子节点的明细模式且存在记录时启用，复用既有查询键与缓存失效；手动刷新同时刷新备注。

不修改日志表、采集/入库路径或审计响应。入站已删除、备注为空、列表尚未加载/请求失败时保留标签，不阻塞审计结果。回归覆盖节点隔离、列表延迟、分页不重复请求及失败降级。
