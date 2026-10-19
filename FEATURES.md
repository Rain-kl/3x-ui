# 二开功能目录

本文记录本项目面向用户的功能、行为和体验改动。括号内的英文标识用于对应源码中的 `FORK(...)` 标记；文件、接口、字段、协议和命令等专有名称保留原文。

## 流量控制与计费

### Xray 原生用户限速（native-rate-limits）

- **背景**：基于 TC/qdisc 的流量整形和按客户端 IP 轮询，无法在 NAT、地址变化及多会话场景下稳定地按认证用户限速。
- **功能**：将面板的 Mbps 下行限速转换为 Xray 核心使用的字节/秒限速。优先级为入站级客户端覆盖值、客户端全局值、入站默认值；另提供按入站共享的下行总带宽组。修改限速后会重启现有连接。启用状态下配置不支持的协议或不兼容的核心版本会明确报错；停用的限速值仍可保存。原 TC 限速已移除。
- **范围与维护**：涉及 `internal/web/service/`、`internal/xray/`、运行时/API 接线和入站、客户端配置界面。上游文件改动使用 `FORK(native-rate-limits)` 标记；移除 TC 的检查纳入移除回归守卫。
- **支持范围**：VLESS、VMess、Trojan、多用户 Shadowsocks、Hysteria 和 MASQUE 支持原生限速；WireGuard、TUIC、MTProto、AmneziaWG 及匿名 SOCKS/HTTP 不支持用户限速。核心配置支持双向限速，面板当前提供下行限速，单位为 Mbps。需要 Xray 核心 `v26.10.10-feat.2` 或更新版本。
- **设计与实现**：[行为规格](docs/specs/native-rate-limits.md)、[实现说明](.agents/notes/native-rate-limits.md)。核心实现基于上游 [PR #6050](https://github.com/XTLS/Xray-core/pull/6050)，原始提交 `da56e92`，移植提交 `4e99a70`；详见 [核心功能说明](https://github.com/Rain-kl/Xray-core/blob/main/FEATURES.md) 和 [核心规格](https://github.com/Rain-kl/Xray-core/blob/main/docs/specs/native-rate-limits.md)。

### 客户端分级限速与入站计费倍率（client-rate-and-traffic-ratio）

- **背景**：不同客户端的带宽套餐不同，同一客户端在不同入站也可能需要不同限速或计费策略。
- **功能**：客户端可设置全局下行限速 `downLimit`，并按入站设置 `downLimitByInbound` 覆盖值。生效顺序为入站覆盖值、客户端全局值、入站默认单客户端限速。客户端列表在桌面表格和移动卡片中显示限速状态，并可查看入站明细。入站的 `trafficRatio`（默认 `1.0`）按倍率计入客户端流量；多节点同步保留限速配置，由各节点本地核心执行。
- **实现说明**：`trafficRatio` 为 `0.5` 时按一半流量计费，为 `2.0` 时按双倍流量计费。客户端表单和列表使用 `downLimit`、`downLimitByInbound` 展示配置和生效值。

#### 批量设置客户端限速

- **功能增强**：客户端列表与分组的批量调整支持设置客户端全局下行限速（Mbps）；留空不变，`0` 清除全局值，逐入站覆盖值保持不变并继续优先生效。对不支持原生限速的活动入站或不兼容的本机核心，批量操作整体拒绝，避免部分客户端被修改。
- **文件与标记**：批量弹窗、表单校验、API 和服务更新路径使用 `FORK(client-rate-and-traffic-ratio)` 标记；13 个 locale 的限速说明同步解释清除全局值后逐入站覆盖/默认值仍可能生效。
- **设计与验收**：[批量限速规格](docs/specs/client-bulk-rate-limit.md)、[限速行为规格](docs/specs/native-rate-limits.md)；[实现说明](.agents/notes/client-bulk-rate-limit.md)。

### 客户端多入站统计与增量计费（multi-inbound-accounting）

- **背景**：客户端同时使用多条线路、入站计费倍率不同或从节点重启时，需要避免流量归属错误、重复扣费及节点明细与总账不一致。
- **功能**：以入站标签和客户端 email 组成内部统计标识，采集后还原面板身份；不改变 UUID、密码和订阅凭据。按入站保存原始用量，客户端总账记录加权后的计费用量；主控保存各入站同步基线，倍率仅作用于新增流量，重复快照不会重复计费。节点计数重置后从新计数继续累计。基线、总账和明细在同一事务中提交，落库失败时回退采集基线以便重试；多入站实时速度按客户端合并展示。
- **维护要点**：`node_client_traffics.inbound_totals` 保存原始入站基线并支持兼容迁移；已有历史总账和明细不自动追溯修正。主控与从节点应共同升级。未升级节点及不提供独立用户统计的协议，无法保证精确的多入站归属；核心异常退出前未采集的内存流量仍可能丢失。

### 客户端单节点流量配额（per-node-client-quota）

- **背景**：不同节点的运营成本不同，需要在客户端总套餐之外，对高成本节点单独设置用量上限，并避免耗尽单节点额度影响其他节点。
- **功能**：客户端可按关联节点设置流量配额，并在客户端编辑界面和列表查看用量、上限、剩余流量及状态。达到节点配额时，仅在该节点断开该客户端，不更改客户端全局启用状态，也不影响其在其他节点的连接和额度。定时周期重置或手动重置时，各节点用量同步清零并恢复连接。订阅展示页保留受限节点，并展示节点配额进度和断流状态。

## 日志审计

### 多节点连接日志与网站流量分析（multi-node-audit）

- **背景**：在主从节点部署中，管理员需要从主控查看各节点客户端访问的网站、双向流量、连接频次和高频行为，无需逐台读取 Xray 访问日志。
- **采集与同步**：Xray 访问日志经进程标准输出传给面板解析，再写入内存队列。子节点以权限 `0600` 的本地暂存文件应对主节点离线，文件上限为 10 MiB，超限时淘汰最旧事件。主节点每 5 秒通过节点鉴权连接最多拉取 1000 条；数据库提交后按事件 ID 确认，并以 ID 去重。单机模式会将本机日志直接写入审计库；主从连接首次同步时持久化子节点身份，并将子节点仍保留的历史明细和汇总统一到主节点。
- **记录与界面**：记录节点、入站、客户端、源地址、目标地址、网站、时间和上传/下载字节。支持节点、客户端和网站筛选，3/12/24 小时窗口，7/30 天每日汇总、UTC 日期筛选、留存设置、确认后清空、网站流量排行、每小时连接趋势、客户端排行、连接明细及高频行为提示。提示仅用于筛查，不自动判断滥用。旧版官方 Xray 缺少连接级字节字段时，界面显示「不可用」，不将缺失值当作零。
- **定制核心与发布**：精确流量计量及 Mihomo X25519-only 兼容需要 `Rain-kl/Xray-core` 的 `v*-feat.*` 稳定版本。面板版本列表优先显示最新 feat 版本；发布流程为各架构固定同一核心 tag，Docker 初始化也会解析最新 feat tag。应先发布核心资产，再构建和发布面板。
- **维护要点**：涉及 Xray 运行时配置、子进程输出、节点同步、数据库、API、路由和侧边栏。同步上游时重点核对 `internal/web/service/xray.go`、`internal/xray/process.go`、`internal/web/job/audit_sync_job.go` 及核心版本和下载来源；保留节点鉴权白名单和 API 契约。应用层字节不包含 TLS/QUIC 传输开销；长连接通常在结束时才产生最终计数；只有 IP 时不能推断域名。

### 嗅探域名归因（sniffed-audit-domains）

- **功能**：保留客户端传入的原始目标地址，并单独记录成功嗅探到的 HTTP/TLS/QUIC/FakeDNS 域名；仅启用路由模式时也可按网站归因。连接明细同时展示域名与原始目标，网站排行优先使用域名，无法识别时使用原始目标。
- **约束与维护**：不自动开启嗅探，不改变转发目标或路由规则；兼容旧日志和节点暂存数据，不回填旧记录。需要定制核心 `v26.10.10-feat.3` 或更新版本，主控与子节点均需升级。上游相关改动使用 `FORK(sniffed-audit-domains)` 标记。
- **设计与实现**：[行为规格](docs/specs/sniffed-audit-domains.md)、[实现说明](.agents/notes/sniffed-audit-domains.md)。

### 审计留存与每日汇总（audit-retention）

- **功能**：默认保留 24 小时连接明细和 30 个 UTC 日的节点、客户端及网站汇总。历史汇总不包含单次访问时间；明细和汇总使用一致的筛选条件。管理员可配置留存时间并确认清理。系统以原子方式压缩明细、显式迁移旧数据，并按 UTC 日期筛选；延迟送达的事件通过稀疏去重处理。
- **设计与实现**：[行为规格](docs/specs/audit-retention.md)、[实现说明](.agents/notes/audit-retention.md)。上游相关改动使用 `FORK(audit-retention)` 标记。

#### 主节点设置与清理同步

- **功能增强**：主节点保存留存设置后广播到启用的子节点；清理日志时广播同一绝对 UTC 水位，清理子节点数据库和内存/磁盘暂存队列。离线或失败节点恢复后在拉取日志前自动重试，旧版本请求不会覆盖新策略，清理水位不会倒退。子节点本地设置入口继续由主节点管理。
- **性能增强**：队列事件仅编码一次，文件刷新和确认在采集锁外写盘；迟到事件批量去重和入库。审计页面采用最多两个并发的一致性读快照，汇总扫描期间日志写入可继续。
- **设计与实现**：[行为规格](docs/specs/audit-retention.md)、[实现说明](.agents/notes/audit-retention.md)。涉及 `internal/audit/`、审计控制器/服务、节点运行时和同步任务，标记为 `FORK(audit-retention)`。

### 主从节点审计数据一致性（audit-node-consistency）

- **功能**：从节点通过经过认证的导出记录并持久化节点身份；恢复仍有效的本地明细后停止子节点队列消费。主节点提交成功后才确认事件，以稳定 ID 去重。子节点界面提示管理员在中央主控查看审计数据。
- **设计与实现**：[行为规格](docs/specs/audit-node-consistency.md)、[实现说明](.agents/notes/audit-node-consistency.md)。上游相关改动使用 `FORK(audit-node-consistency)` 标记。

### 审计工作台与内部连接过滤（audit-workbench）

- **功能**：审计页面复用管理页面的布局、卡片和明暗/Ultra 主题样式。移除项目赞助商展示位、侧边栏入口、赞助商页面和公开赞助商 API，以及对应的翻译和文档；MTProto Telegram 赞助频道标签仍作为独立协议能力保留。过滤保留的 `api` 入站管理连接，避免其进入暂存、入库和查询，并定期清理历史控制连接记录；真实客户端访问本地地址仍会保留。
- **设计与实现**：[行为规格](docs/specs/audit-workbench.md)、[实现说明](.agents/notes/audit-workbench.md)。上游相关改动使用 `FORK(audit-workbench)` 标记。

- **入站备注展示**：审计明细在原始标签后显示当前入站备注。前端复用轻量入站列表，按节点与标签建立 Map，分页和日志轮询直接映射；备注不写入日志表。缺失备注时保留原标签。

- **IPv6 日志解析修复**：采集路由标签时从完整目标地址之后读取，避免 IPv6 地址的方括号导致入站/出站为空；适用于新采集日志，不回填已入库数据。修改 `internal/audit/spool.go`，标记 `FORK(audit-workbench)`，设计与实现沿用本条目的文档链接。

- **网站排行增强**：网站流量排行标题提供「更多」，弹窗沿用主页面筛选并查询完整网站排行，支持每页 20/50/100 条（默认 20）。后端仅返回当前页，流量相同按网站名稳定排序，复用有界读快照。

- **排行布局优化**：网站排行与客户端排行卡片等高，移除小时连接趋势图；客户端排行按连接数排序，通过分页查看全部客户端，支持每页 20/50/100 条并独立上下滚动，遵循主页面全部筛选、保留点击客户端筛选。新增 `/audit/clients` 分组分页查询，复用排行快照与响应结构。修改 `frontend/src/pages/audit/AuditClientsCard.tsx`、`AuditPage.tsx`、`AuditPage.css`、审计服务/控制器、语言与 API 文档，标记 `FORK(audit-workbench)`，设计与实现沿用本条目的文档链接。

- **设置布局优化**：审计设置弹窗在桌面按两列排列留存时间与速率阈值，清理操作横向排列；窄屏转为单列，弹窗内容超出视口时可滚动。修改 `frontend/src/pages/audit/AuditSettingsModal.tsx`、`AuditSettingsModal.css`，标记 `FORK(audit-workbench)`。

#### 连接与网站速率、高频阈值

- **功能增强**：「已计量连接」卡片改为最近一分钟的连接/秒与网站/秒；新增最近 24 小时分钟速率图，沿用节点、客户端和网站筛选，可通过客户端筛选区分。网站数按一分钟内不同网站计数，重复访问不重复增加。基于已收到的连接完成日志，界面提示同步延迟。
- **高频判定**：按客户端最近 60 秒平均速率比较可配置阈值，默认连接 10/秒、网站 1/秒，任一达到即提示，设为 0 可关闭对应检测。阈值随主节点审计策略同步；不再用累计连接数或累计网站数触发告警。
- **性能与展示**：分钟计数在入库事务中批量聚合、按事件 ID 去重，固定保留 24 小时，图表不重复扫描原始日志；支持旧明细一次性迁移回填及日志清理。修复留存提示中花括号占位符未替换的问题。涉及审计采集服务、模型/迁移、策略同步、设置与图表、语言文件和 API，使用 `FORK(audit-workbench)` 标记，设计与实现沿用本条目的文档链接。
- **迁移兼容**：分钟计数表同时登记到跨数据库迁移清单，SQLite → PostgreSQL 迁移时保留 24 小时速率历史；沿用模型清单一致性校验防止遗漏。修改 `internal/database/migrate_data.go`，标记 `FORK(audit-workbench)`。

## 多节点管理

### 从节点 ApiToken 自动登录（node-token-login）

- **背景**：管理员从主控节点打开从节点面板时，无需再次手动输入账号密码。
- **功能**：主节点提供 `/panel/api/nodes/loginUrl/:id`，节点列表据此获取从节点登录链接并在新标签页打开。从节点入口（`GET /`）校验 URL 中的 `apiToken`；仅启用且具有 `admin` 权限的令牌可创建会话并跳转至 `/panel/`。

### 从节点面板与 Xray 远程重启（remote-node-restart）

- **背景**：管理员可在中央控制台处理从节点配置变更或运行异常，无需逐台通过 SSH 操作。
- **功能**：主节点通过受信通道（mTLS/Node Token）调用从节点 `/panel/api/setting/restartPanel`。从节点执行面板热重启并带动 Xray 进程重启；节点列表提供重启操作和二次确认。

### 控制端权威的代理出站同步（authoritative-outbound-sync）

- **背景**：多节点环境需要从控制端统一管理代理出站，并隔离单节点同步故障，兼容未实现同步接口的旧版或官方节点。
- **功能**：识别 VMess、VLESS、Trojan、Shadowsocks、Hysteria、Hysteria2、WireGuard、SOCKS 和 HTTP 代理出站；同步时仅替换代理出站并保留各节点的 `freedom`、`blackhole`、`dns` 等系统出站。控制端为权威来源，其新增、修改和删除会单向同步至从节点；从节点本地新增项不会回传。各节点推送独立执行，单节点失败不阻塞其他节点；旧节点返回 404 时清除脏标记，避免重复重试。标签为 `warp`、`warp-*` 或 `warp_*` 的 WARP 出站保留在各节点本地，不参与同步。

### 跨节点路由配置与节点级标签隔离（node-scoped-routing）

- **背景**：管理员需要从主控管理各节点路由，同时避免将其他节点的入站或出站标签误用于当前节点。
- **功能**：路由界面可选择本机或从节点，并查看、新增、编辑、删除、排序、导入和导出目标节点的规则；保存后通过远程 RPC 下发。从节点视图只显示本节点入站及其备注，出站列表只显示节点模板中存在的标签（含 `direct`、`blocked` 等系统出站）。控制端用于去重的 `n{id}-` 入站前缀只用于控制端展示，下发时还原为从节点原生 tag。`POST /panel/api/xray/` 响应增加 `outboundTags` 字段。

- **即时生效增强**：远程保存路由与子节点本地保存使用同一配置协调入口，优先热更新；规则内容相同也会重试运行态对齐。应用失败明确回传主节点，已停止的核心不会因保存被启动。详见[行为规格](docs/specs/node-routing-immediate-apply.md)和[实现说明](.agents/notes/node-scoped-routing.md)，上游标记为 `FORK(node-scoped-routing)`。

### 主节点路由规则同步（routing-rule-sync）

- **背景**：一主多从部署需要统一维护网站分流规则，避免逐节点复制和保存。
- **功能**：主节点本机规则可开启「同步」，保存后向全部启用子节点下发权威快照；关闭同步或删除规则会撤回副本。同步组由主节点管理，位于 API 控制规则之后、本地规则之前；保留子节点原有规则和其他路由设置。运行核心优先热更新，停止核心保持停止。离线、失败或新增节点每 30 秒自动重试，重复版本不重复应用。
- **约束**：仅同步规则；引用的入站、出站、均衡器标签必须在各子节点存在，否则报告具体节点与缺失引用。主从面板均需升级；旧版接口不做覆盖式降级。API 与均衡器内部控制规则不参与同步。
- **范围与维护**：涉及路由编辑器、Xray 模板保存和面板字段剥离、节点运行时、控制器鉴权与重试任务；新增 `internal/routingsync/`、`internal/web/service/routing_sync.go`。上游相关改动标记为 `FORK(routing-rule-sync)`。
- **设计与实现**：[行为规格](docs/specs/routing-rule-sync.md)、[实现说明](.agents/notes/routing-rule-sync.md)。

### 本地前置路由（routing-local-pre）

- **背景**：子节点来源 IP 拒绝规则不能被主控同步的目标网站直连规则提前放行。
- **功能**：规则可设为「本地前置」，执行顺序为 API、本地前置、主节点同步、普通本地；与同步互斥，不参与广播，保存、同步刷新与撤回均保留该组。桌面及移动端展示类型，编辑后立即按组重排，禁止拖动越组。
- **范围与维护**：路由模板保存/合并、核心字段剥离、规则表单与列表；标记 `FORK(routing-local-pre)`。
- **设计与实现**：[行为规格](docs/specs/routing-rule-sync.md)、[实现说明](.agents/notes/routing-local-pre.md)。

### 节点列表批量操作（node-batch-operations）

- **背景**：桌面端节点操作按钮过于拥挤，且批量操作无法涵盖离线节点删除。
- **功能**：桌面端将探测、重启、编辑和删除操作收纳到更多菜单；在线节点仍保留更新快捷入口。工具栏支持批量更新、重启和删除；离线节点可参与批量删除，更新和重启仅作用于已启用且在线的节点。

## 定制核心版本管理

### 仅使用最新定制 Xray 核心（custom-core-only）

- **功能**：面板安装、升级和手动更新统一使用 Rain-kl/Xray-core 最新 `-feat.x` 发布版；移除版本锁定、官方核心和任意旧版切换。更新前先下载并校验核心，成功后再停止现有核心。版本列表自动获取并优先展示最新 feat 版本。
- **发布验证**：核心发布前验证审计、嗅探域名、原生限速及竞态行为；面板发布前验证实际打包核心的版本、校验和、网站审计、限速及 gRPC，并执行前端检查。验证失败则停止发布。
- **设计与实现**：[行为规格](docs/specs/custom-core-only.md)、[实现说明](.agents/notes/custom-core-only.md)。上游相关改动使用 `FORK(custom-core-only)` 标记。

## 定向订阅通知 (subscription-notifications)

- **需求背景**：管理员需要向指定客户端或用户分组发布通知，并可让代理客户端在节点列表中看到通知。
- **修改内容**：侧边栏新增通知管理，支持可选标题（留空只发正文，网页、占位节点名称与 Telegram 投递不再出现标题行、分隔符或空行）、纯文本正文、接收对象、排序、保存草稿、发布、编辑、撤回和删除。客户端与用户组可同时选择，按并集匹配且去重；两类选择器提供全选和清空，客户端全选覆盖所有页面、保存点击时的目标快照。分组按当前成员动态匹配；同一订阅命中多次只显示一次。通知使用独立数据表，批量匹配受众，不创建真实入站或下发代理配置。
- **订阅行为**：网页在普通配置之前显示完整通知；可选在 raw/base64、Xray JSON、Clash/Mihomo 中添加按顺序排列的标准 VLESS 通知占位节点，地址统一为 `127.0.0.1:1`。不提供真实代理服务，JSON 用 blackhole 路由阻断转发，Clash 通知不进入实际代理选择组。撤回、编辑和删除在下次订阅请求生效。
- **修改文件**：`internal/database/model/subscription_notification.go`、模型及跨库迁移注册、`internal/web/service/subscription_notification.go`、`internal/web/controller/subscription_notification.go`、`internal/sub/notifications.go`、订阅输出、前端侧边栏与 `pages/notifications`、订阅网页、接口文档与语言词条。
- **上游改动标记**：`FORK(subscription-notifications)`。
- **设计文档**：[定向订阅通知规范](docs/specs/subscription-notifications.md)；**实现文档**：[Agent Note](.agents/notes/subscription-notifications.md)。

### 普通发布与 Telegram 推送分开

- 通知编辑器和列表新增“发布并推送 TG”。普通发布默认不主动推送 Telegram；仅显式点击新按钮才在保存事务内向匹配的绑定用户排队发送，Bot 未配置或未启用时明确报错。请求标志不入库，避免后续普通保存继承推送意图。修改通知前后端、Telegram 入队与 API 契约，使用 `FORK(subscription-notifications)`，设计与实现见本节链接。

## mixed 统一客户端管理（mixed-clients）

- **需求背景**：主控无法向子节点部署 mixed，独立静态账号无法复用客户端的计费、限速、到期和撤销流程。
- **修改内容**：mixed 接入本机及子节点部署、编辑和克隆；统一使用客户端关联及 username/password 凭据。HTTP/SOCKS5 共用动态身份，支持实时改密及撤销，最后一个账号删除后仍强制认证。旧 accounts 保存/导入时幂等转换；匿名模式保留但不可关联客户端。核心最低版本为 `v26.10.10-feat.4`。
- **订阅行为**：自动链接、JSON、Clash 排除 mixed；订阅展示页独立提供该用户可用 mixed 节点及 HTTP/SOCKS5 地址、用户名和密码的复制入口，凭据与 IPv6 正确编码。控制端客户端二维码弹窗同样提供 mixed 手动配置折叠项，复用已有数据和分享地址策略，支持仅关联 mixed 的客户端。
- **修改文件**：数据库 Client/ClientRecord 与迁移、`internal/web/service/inbound_mixed.go`、客户端及 Runtime 分发、`internal/xray` 配置/API/版本检查、`internal/sub/mixed_manual.go`、前端入站及客户端表单、订阅页、生成接口模型和 13 种语言词条。
- **上游改动标记**：`FORK(mixed-clients)`；定制核心的对应改动见 Rain-kl/Xray-core 同名条目。
- **设计文档**：[mixed 客户端规范](docs/specs/mixed-clients.md)；**实现文档**：[Agent Note](.agents/notes/mixed-clients.md)。

## 用户 Telegram Bot（telegram-user-bot）

- **需求背景**：用户需要通过 Telegram 绑定购买的客户端、查询订阅和用量、接收定向通知并联系管理员；管理员需要持久化的双向聊天及排障日志。
- **修改内容**：新增独立用户 Bot，消息中包含客户端核心订阅标识（sub_id）即绑定、也支持 30 分钟一次性绑定码，不校验协议/域名/路径，账号与客户端双向唯一。支持获取/确认重置订阅、查询剩余流量与天数、列出订阅节点名、绑定/确认解绑。订阅回复附可用 MIXED 连接及 Clash/Singbox 转换提示，发送成功30秒后请求删除，删除任务持久化并有界重试。独立绑定表保持旧管理 Bot 兼容。仅使用 Telegram 原生 `/` 指令提示，移除底部快捷键盘，并在普通指令响应时收起旧键盘。移除清除 Token 开关，留空保留、填写新值替换。新增默认关闭的「开启匿名聊天」：关闭时未绑定账号只处理指令及绑定，普通消息和附件不转发、不计待回复未读；待发转发同样重新检查。
- **管理与聊天**：侧边栏 Telegram 含消息、日志、设置三个页签；分页会话、未读、文字/附件、指定消息回复、网页主动发送。非指令咨询通过既有管理员 Bot 转发，管理员回复原转发消息即可定向答复。跨 Bot 附件下载后上传，不暴露 Token。
- **通知与维护**：发布订阅通知时仅向命中客户端或分组的绑定账户排队；新绑定成功后按通知顺序补发该客户端当前可见的历史已发布通知，重复提交已有绑定不重发；支持 `/notifications` 指令主动重新收取全部当前可见的已发布通知；发送前重查版本和绑定，咨询优先，支持限速和有界重试。收发消息和运行错误入库，独立保留周期及手动清理；不删除绑定或待发送/发送中消息。消息与日志保留原文（绑定凭据、订阅链接不脱敏），仅接口错误中的 Token 与代理凭据脱敏；管理接口沿用管理员权限及 CSRF。
- **绑定风控**：同一 Telegram 账号 10 分钟内最多 30 次绑定尝试（覆盖 `/bind` 带参、`/start` 带绑定码、直接发送绑定码或订阅链接及待绑定输入），超限立即持久化拉黑；拉黑账号的全部消息与回调一律忽略、队列消息取消，解除需管理员清理黑名单记录。
- **订阅提示**：获取或重置订阅链接后，提示 Clash 用户使用 `https://sublink.osur.eu/` 转换订阅。
- **修改文件**：`internal/telegramuser/`、`internal/database/model/telegram_user.go`、两处模型注册、Web 生命周期/控制器、Service 适配、管理员 Bot 回复路由、通知发布事务、`frontend/src/pages/telegram/`、侧边栏/路由、13 种语言词条与生成接口文档。
- **上游改动标记**：`FORK(telegram-user-bot)`。
- **设计文档**：[用户 Telegram Bot 规范](docs/specs/telegram-user-bot.md)；**实现文档**：[Agent Note](.agents/notes/telegram-user-bot.md)。

### 订阅详情、节点名与短时回复（telegram-subscription-details）

- **需求背景**：用户需要在 Bot 中获取 mixed 手动连接信息与全部订阅节点名，并减少订阅凭据长期留在聊天中的时间。
- **修改内容**：`/subscription` 和重置后的回复按指定格式返回订阅、可选 mixed 和转换提示；发送成功30秒后请求删除，网络失败有界重试。`/nodes` 仅返回订阅节点名并加入原生 `/` 菜单，包含手动 mixed，不包含通知或状态占位。
- **修改文件**：`internal/telegramuser/subscription.go`、接收器及菜单、`internal/sub/telegram_details.go`、订阅生成名称收集点、Service Hook、`internal/database/model/telegram_message_deletion.go` 及两处模型注册。复用当前订阅过滤和地址策略，独立持久化删除队列并按 Bot 身份隔离。
- **上游改动标记**：`FORK(telegram-subscription-details)`。
- **设计与实现**：[功能规格](docs/specs/telegram-user-bot.md)、[Agent Note](.agents/notes/telegram-subscription-details.md)。

### 用户 Bot 自定义指令（telegram-custom-commands）

- **需求背景**：管理员希望配置常见问题和服务说明，让用户通过 Telegram 原生 `/` 菜单直接获取固定内容。
- **修改内容**：Telegram 新增「自定义指令」Tab，支持新增、编辑、启停、删除；配置名称、菜单说明、纯文本回复。保存后回复立即生效；启用项与7个内置项一起通过 `setMyCommands` 显示在用户 `/` 菜单，停用或删除同步移除。菜单通常在下一轮轮询更新，Telegram 客户端可能有缓存。
- **边界**：最多93项，名称规范化并禁止重复和覆盖内置项；所有用户均可使用，沿用黑名单和完整消息日志。事务保存独立数据表，跨库迁移保留；不重启 Bot。
- **修改文件**：`internal/telegramuser/custom_commands.go`、接收器/控制器、`internal/database/model/telegram_custom_command.go`、两处模型注册、`frontend/src/pages/telegram/TelegramCommands.tsx`、Telegram 页签/API、生成模型/接口文档和13种语言词条。
- **上游改动标记**：`FORK(telegram-custom-commands)`。
- **设计与实现**：[功能规格](docs/specs/telegram-custom-commands.md)、[Agent Note](.agents/notes/telegram-custom-commands.md)。

### 长轮询恢复状态

- `getUpdates` 客户端总超时60秒，服务端等待25秒，为连接和代理留出余量；失败自动退避重试。恢复后清除对应旧错误，历史失败与恢复事件保留在日志，不清除其他故障。修改 `internal/telegramuser/api.go`、`runtime.go`；标记 `FORK(telegram-user-bot)`，规格与实现见本节链接。

## 审计数据 ZIP 导出 (audit-export)

- **需求背景**：分页页面无法方便地取得完整网站流量、指定客户端网站访问数据和日志库档案。
- **修改内容**：审计页新增数据导出，支持全部网站、指定客户端和日志库全部数据，打包 ZIP 下载。网站采用 CSV，包含连接数及上传、下载、总字节；原始详细记录、每日汇总、分钟统计、带宽分钟数据和去重记录采用 JSONL，并附 UTC 范围说明。导出全部保留日期和节点，不使用页面筛选；子节点从主节点导出。游标读取、一致性快照、单并发、超时取消、临时文件清理避免全量内存占用。
- **修改文件**：`internal/web/service/audit_export.go`、`audit_query_snapshot.go`、审计控制器、`frontend/src/pages/audit/AuditExportModal.tsx`、接口文档和语言词条。
- **上游改动标记**：`FORK(audit-export)`。
- **设计与实现**：[功能规格](docs/specs/audit-export.md)、[Agent Note](.agents/notes/audit-export.md)。

## 本地 OpenCode AI 助手 (panel-ai)

- **需求背景**：管理员希望使用自然语言分析日志和疑似滥用，并安排定时分析后推送 Telegram；复用已有 OpenCode 的模型与 agent 能力。
- **修改内容**：侧边栏新增 AI，包含对话、任务、设置。设置页一键部署经过 SHA-512 校验的官方 OpenCode v2.0.32 独立包并自动配置专用目录；对话采用官方 Web 界面（hash 路由适配面板非根路径），面板鉴权代理仅监听本机、随机密码保护的原生服务，支持流式对话及工具展示；定时任务复用原生 ACP。可在网页直接编辑 opencode.json、添加自定义模型供应商并完整查看/修改密钥；无需自行部署 HTTP 服务、Node/npm 或预先配置运行环境。提供只读 DQL SQL 审计 MCP 工具，直接在数据库聚合、排名和窗口分析，避免逐用户分页读取；给 agent 注入数据库方言、六张日志表字段含义、时间/流量单位和去重规则，禁止整表读取到上下文。支持 AI 按管理员要求创建自然语言定时任务；任务可编辑、暂停、删除和立即运行，默认间隔12小时，可选择向管理员 Telegram 推送结果。会话与任务运行持久化，支持取消、工具轨迹、独立保留时间及清楚的分析/通知错误；不新增模型提供商或自研 agent 循环。
- **修改文件**：`internal/panelai/`、数据库模型/迁移注册、`internal/web/service/panel_ai.go`、控制器和生命周期、`internal/telegramuser/report.go`、`frontend/src/pages/ai/`、导航、生成接口文档及语言词条。
- **上游改动标记**：`FORK(panel-ai)`。
- **设计与实现**：[功能规格](docs/specs/panel-ai.md)、[Agent Note](.agents/notes/panel-ai.md)。

## 审计实时带宽曲线 (audit-bandwidth)

- **需求背景**：连接结束才入库的访问日志无法判断长连接是否持续占用带宽，需要观察客户端的实际上传、下载速率。
- **修改内容**：日志审计新增带宽图表，每5秒刷新当前速率，支持按节点、客户端筛选；可切换最近1小时实时曲线和24小时分钟趋势；后端保存最近1小时细粒度采样，5秒刷新，浏览器刷新保留峰值；不再拼接分钟均值，面板进程重启或审计清理后重新采样。使用原始流量计数及实际采样间隔，不受计费倍率影响。清零、重启或同步中断时重新采样，缺失画为断点。卡片明确说明网站和访问日期筛选不影响带宽。
- **性能与维护**：复用既有本地及子节点流量同步，不新增节点请求；每节点至多4秒批量读取一次，不逐用户查询。分钟数据每30秒批量持久化，固定保留24小时，完整 ZIP 导出和日志清理覆盖新表；AI 审计工具也可查询当前带宽与历史趋势。
- **修改文件**：新增 `internal/web/service/audit_bandwidth*.go`、`internal/database/model/audit_bandwidth.go`、`frontend/src/pages/audit/AuditBandwidth*.tsx`；接入流量同步、模型注册及跨库迁移、审计 API/导出/清理、Web 生命周期、AI 工具、接口文档与13种语言词条。
- **上游改动标记**：`FORK(audit-bandwidth)`。
- **设计与实现**：[功能规格](docs/specs/audit-bandwidth.md)、[Agent Note](.agents/notes/audit-bandwidth.md)。

## Telegram 邀请码管理（telegram-invite-management）

- **背景与功能**：邀请码不再只在一次弹窗中展示；Telegram 设置中的邀请码列表固定显示当前码、绑定链接、有效期及有效/已使用/已过期/已吊销状态，可复制、轮转或吊销。仍为一次性，默认30分钟且支持自定义或永不过期；轮转立即撤销旧码，旧版哈希码提示重新生成。
- **自动绑定**：完整 `/start bind_…` 自动绑定，无需 `/bind`；裸 `/start` 明确提示参数缺失并记录诊断。已验证的本地重发流程不能用来断言此前客户失败的现场原因。
- **文件与标记**：Telegram 模型、绑定/维护/接口、前端绑定页及新增邀请码组件、语言/API，使用 `FORK(telegram-invite-management)`。
- **设计与实现**：[规格](docs/specs/telegram-user-bot.md)、[说明](.agents/notes/telegram-invite-management.md)。

### 自定义绑定链接有效期

- 生成与轮转支持按分钟、小时、天自定义时长或选择永不过期；默认30分钟，有限期限为1分钟至3650天。复用 expires_at=0 表示无限期，无新表或列；保持一次性及轮转/吊销。前端统一弹窗、列表与 API 文档同步更新；使用 `FORK(telegram-invite-management)`，设计与实现见本节链接。

## 订阅重置撤销旧代理凭据（telegram-reset-revoke）

- **背景与功能**：仅更改订阅 URL 不能撤销已导入的代理配置。Bot `/reset` 同时更换订阅标识和附加入站的认证凭据，先停用旧身份，逐入站更新，并在远端确认后恢复原启用状态；失败尝试保持停用并返回具体错误，不把排队同步视为成功。保留用量、配额、到期、地址、分组及绑定。
- **协议兼容**：轮换 UUID、密码、Hysteria Auth、MTProto Secret、WG/AWG 密钥；保留各隧道地址。不同 Shadowsocks cipher 与 MTProto 域名使用各入站的凭据，订阅导出与运行配置一致。不支持客户端级认证的协议明确拒绝；已经建立的会话能否立即终止取决于协议及现有断开设置。
- **文件与标记**：客户端重置与运行态确认服务、Bot 订阅接口/提示、订阅凭据投影，使用 `FORK(telegram-reset-revoke)`。
- **设计与实现**：[规格](docs/specs/telegram-user-bot.md)、[说明](.agents/notes/telegram-reset-revoke.md)。

## 订阅访问审计与泄露风控（subscription-access-guard）

- **背景与功能**：记录有效订阅的客户端、来源 IP、UTC 时间、方法、格式和状态；管理员在日志审计中分页筛选、查看风险/通知错误及清理访问日志。所有订阅输出统一保护，未知随机订阅不落库。
- **风控**：最近 10 分钟第 6 个不同 IP 的 GET 请求立即被阻断；复用客户端停用与节点 Runtime，失败持久化并重试。只向相关客户端当前绑定的 Telegram 账号发送停用提示，并向管理员 Bot 告警；通知独立于用户 Bot 轮询，持久化、去重、有界重试，失败可手动重发。HEAD 记录但不触发阈值。
- **安全与维护**：只信任显式配置代理的 IP 头，规范化 IPv4/IPv6；IP 窗口与风险事务化写入、重启保留；访问日志保留 30 天，清理不解除风控。管理员通过客户端管理恢复启用后，下次访问开始新窗口。完整审计 ZIP 包含访问与风控数据。
- **文件与标记**：新增访问/窗口/风险/通知模型及双迁移登记、服务/子服务器中间件/审计接口、Telegram 安全通知、维护任务、审计弹窗、语言/API；使用 `FORK(subscription-access-guard)`。
- **设计与实现**：[规格](docs/specs/subscription-access-guard.md)、[说明](.agents/notes/subscription-access-guard.md)。
