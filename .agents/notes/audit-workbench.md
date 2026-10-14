# Workbench integration

Audit was missing from shared CSS selectors and omitted dark/ultra root classes, which caused the visual mismatch. Extend the existing styles rather than create a new theme. Drop reserved api inbound control-plane events at capture, ingestion and query boundaries; pruning clears old records. Keep legitimate client loopback traffic and all existing client metrics. Remove project sponsor UI routes, dashboard/login/sidebar placements, and the public sponsor API. MTProto sponsored-channel tags remain separate.


## 入站备注仅在前端展示

审计明细通过共享 useInboundOptions 获取轻量入站列表，保留原始日志标签，并在渲染时追加当前备注。Map 按节点编号分组，子节点标签只剥离对应的 n{id}- 前缀一次，避免跨节点同名标签串备注；本机标签不剥离前缀。useMemo 仅在入站列表引用变化时构建，分页和 15 秒审计轮询只查 Map。请求在非子节点的明细模式且存在记录时启用，复用既有查询键与缓存失效；手动刷新同时刷新备注。

不修改日志表、采集/入库路径或审计响应。入站已删除、备注为空、列表尚未加载/请求失败时保留标签，不阻塞审计结果。回归覆盖节点隔离、列表延迟、分页不重复请求及失败降级。

## 完整网站流量排行

首页仍只显示前 20 个，标题右侧「更多」打开独立弹窗。弹窗挂载后才调用 audit/sites，所有时间/日期/节点/客户端/网站筛选直接来自主页面已应用状态。每页 20/50/100，默认 20，分页在服务器执行；筛选变化时派生第一页，避免 effect 重置导致先发送旧页码请求，已选页大小保留。主页面重新查询期间不卸载弹窗，防止页大小意外重置。

后端复用 auditMetricsQuery 与只读一致性快照，只查询网站分组数量和当前页，不生成客户端排行/趋势/访问明细。首页与完整排行共用 auditSiteRanking，流量并列时按网站名称稳定排序。参数限制每页最多 100 并检查偏移溢出。无新增日志持久化字段；已有保留期、主子节点模式和控制流量过滤不变。

验收覆盖超过 20 个网站的全部分页、20/50/100、稳定并列排序、首页与弹窗排行一致、所有筛选及历史汇总、错误/空结果、关闭时不请求、请求参数与真实页码、筛选重置第一页并保留每页数量。分页控件独立于表格，因此空结果也可选择数量。

## 一分钟速率与24小时趋势

rates 固定读取已到达且在滚动 60 秒内的有效连接日志，连接数与不同网站数分别除以 60。高频客户端只来自相同窗口，SQL 按客户端聚合并使用可配置阈值过滤，最多 20 项；历史总量接口保留空 alerts 兼容字段，移除旧累计启发式查询。时间口径是核心连接完成日志的时间，不是实时建立连接数；节点延迟在界面明示。时间窗口固定，节点/客户端/网站筛选跟随页面已应用状态。

24 小时趋势使用 AuditMinuteStat，按 UTC 分钟/节点/客户端/网站保存连接次数。每批原始 INSERT 用 RETURNING 获得真正接受的 ID，迟到归档沿用回执去重；同一事务批量 upsert 分钟计数，重试和同批重复 ID 不会翻倍。查图只扫描分钟计数并分组，补齐 1440 个完整分钟，当前未完成分钟由实时卡片体现；网站数量跨节点与客户端按 DISTINCT 去重。分钟表有时间主键及节点/客户端时间索引，明细增加客户端时间索引，避免最近一分钟按客户端筛选扫描全部历史。

首次数据库迁移在事务中从尚存明细回填并保存 auditRateAvailableFrom，本地覆盖起点不随父节点策略广播。随后启动不回填，明细留存变化不影响已有分钟历史，分钟过期纳入既有五分钟维护，无新增定时任务。清理覆盖分钟表并使用既有清理水位阻止旧日志复活；不足24小时的旧历史或清理后的区间显示可用起点，不画成零。

当前速率15秒刷新，图表60秒刷新，避免每次轮询传输全部1440点；客户端筛选由服务器执行。图表复用 Sparkline 的交互、主题、坐标与阈值参考线。设置新增有限非负阈值，0关闭，默认10/1；旧API省略阈值保留原值，版本同步沿用现有失败报告与重试。旧测试夹具更新为新设置响应与独立速率请求，未新增或运行测试。静态检查及构建结果见提交说明。

应用 i18n 使用单层花括号 prefix/suffix；只修正所有语言的审计 retentionHint/clearOlderConfirm，保留订阅模板与机器人模板自身的双层括号语法。

## IPv6 地址不再抢占路由块

Capture 原先从整行找第一组方括号，IPv6 源地址或目标地址会先于真实路由块被读到，导致入站/出站为空。改用同一次访问日志正则的匹配下标截取源/目标，并从完整目标后的文本读取紧邻路由块。没有新增正则扫描或数据库查询，仍支持两种路由箭头；没有路由块时不从后续邮件/其他元数据猜测标签。按用户要求不处理已经入库的数据。
