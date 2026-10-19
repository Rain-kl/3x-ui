# SQL 审计分析工具（2026-10-19）

- 用 execute_sql 取代提供给模型的分页 query 工具；保留 create_task。支持单条本机数据库方言的 SELECT/WITH，包括聚合、CTE、子查询、窗口与排序。SQL 直接在数据库计算，只返回有界结果，不按每个客户端循环分页获取历史。
- 仅查询日志审计表：audit_events、audit_daily_stats、audit_minute_stats、audit_bandwidth_minutes、audit_receipts、subscription_accesses；不扩展到账号/密钥等业务表。SQLite 使用真实连接 authorizer 限制读表、禁止写操作/ATTACH/PRAGMA/扩展加载及文件函数；PostgreSQL 使用只读事务、语法树与原生 EXPLAIN 实际计划验证日志基表及内置非 volatile 函数/运算符，禁止写 CTE、SELECT INTO、锁定与其他语句。
- 执行共用审计查询并发槽（最多2），30秒超时，SQL最多32KiB，返回最多200行/64列/256KiB，超限标记 truncated 并要求精化SQL，不通过分页整库拉取。SQLite 使用独立 mode=ro/query_only 连接及 authorizer，查询结束关闭；取消/异常均清理，不更改主库连接池权限。
- 官方 Web agent、原生任务以及 MCP initialize 环境信息都包含数据库方言、当前UTC时间、保留策略、日志表字段/含义、时间与流量单位、归档去重条件和SQL分析示例。禁止 SELECT * 或分页枚举整表；优先时间过滤、GROUP BY/HAVING/窗口排名，输出证据、覆盖情况与不确定性。
- 详细记录与日汇总经事务归档保持互斥；延迟子节点汇总可能位于近期日期，按真实保留与日期粒度合并，audit_receipts 仅用于防重，不计连接。分钟连接数不能当并发连接数；带宽必须用原始分钟字节与 covered_seconds，不能拿关闭连接的计费流量估计实时带宽；未知流量 NULL 不视为已计量0。
- 验收：SQLite/PG聚合、WITH和窗口可用；写入、堆叠语句、写CTE、非日志读表、危险函数失败；超时取消/截断不污染连接；两种对话入口和定时任务均用同一工具和环境上下文。

---

# OpenCode v2 官方界面接入（2026-10-19）

本节替代下文 v1 自制对话及 CLI-only 的旧规则。管理员已授权改用 OpenCode v2 官方 Web 界面、本机服务及面板反向代理。

- 固定官方 @opencode/cli 2.0.32 独立二进制，按平台下载 npm 官方发布包并校验 SHA-512；不要求宿主机 Node/npm。v1 安装需通过部署按钮升级，不能静默当作 v2 使用。
- 官方 Web 源码固定 v2.0.32，仅适配动态面板路径、官方 hash 路由（上游按根路径解析 pathname，不能使用 Router base）、同源 API/附件地址、CSRF 请求头和禁用 service worker；保留官方聊天、会话、模型与工具界面，不再展示面板 AIChat。构建产物随面板提供，记录来源、许可证和重建脚本。
- OpenCode 服务只绑定 127.0.0.1 随机端口，使用每次启动随机密码；代理剥离浏览器 Cookie/Authorization 后注入服务认证，密码不下发浏览器、不放 URL、不记日志。停止、禁用或修改连接/供应商配置时关闭进程；异常退出后允许重新启动，启动失败返回具体错误。
- 官方界面和所有资源/API/事件流/WS 必须逐请求验证面板管理员会话，不允许监控 Token、节点 Token 或仅知道 URL 的匿名访问。代理请求严格限定固定本地服务，拒绝跨源 Origin；所有写请求校验现有 CSRF token，WS 握手要求同源 Origin。不能代理认证/配对入口导出独立服务凭据。响应 no-store、禁止跨站嵌入；仅允许面板同源 iframe，不注册 service worker、不缓存审计数据。
- 代理支持 SSE 即时刷新和 WebSocket，实时 API 不受普通 30 秒写超时/压缩缓冲限制；官方静态脚本和 WASM 保留 gzip 压缩，避免传输完整未压缩资源；面板 base path 可非根路径。官方模型配置采用 providers/package/settings，兼容读取旧 provider/npm/options 后显式迁移保存，不猜测 ID 大小写。
- 审计 MCP 仍由面板提供随机 bearer、只读有界查询和创建任务工具；官方对话和定时任务使用同一专用工作区与供应商配置。自然语言任务、任务运行记录、Telegram 推送保留面板实现，执行复用官方 ACP。
- 验收：无登录、登录失效、低权限 Token、跨域写请求不能访问服务；随机服务密码不出现在页面/API；官方 UI 在随机面板路径正常加载；模型/流式文字/工具可使用；修改设置/关闭面板释放本机进程；新装 v2 与旧配置迁移可用。

---

# 面板 AI：复用本地 OpenCode

## 架构与所有权

用户已确认采用 OpenCode 原生 CLI 的 ACP 标准输入输出协议。面板不实现 agent 循环、模型提供商或推理运行时：OpenCode 持有模型配置、会话消息、工具调用和上下文；面板持有连接设置、会话引用、自然语言定时任务及运行结果。侧边栏 AI 进入「对话、任务、设置」。模型列表由原生 CLI 提供，网页配置独立 OpenCode 配置文件，仅管理员可读取完整配置。

## CLI 部署与生命周期

- AI 设置提供“一键部署 OpenCode”，下载固定版本 1.19.3 的官方独立发布包，验证 SHA-256、解压并检查版本后原子安装到面板数据目录；支持 Linux amd64/arm64、macOS amd64/arm64、Windows amd64。失败保留已安装版本并展示具体步骤和错误。不执行远程 shell 安装脚本，不依赖系统 Node/npm、环境变量或预先运行的 HTTP 服务。
- 部署是有状态后台任务，页面显示下载、校验、安装阶段；防止重复部署。成功自动配置绝对 CLI 路径、专用工作目录并启用 AI；供应商密钥需管理员自行填写，不编造可用模型。
- 可手动指定 CLI 与专用目录。旧 HTTP URL、账号、密码不再参与运行；保存新设置时丢弃旧连接配置。每次运行启动 `opencode acp --pure`，通过 stdin/stdout 的原生 ACP JSON-RPC 通信，结束/取消/超时关闭拥有的进程，不连接外部 HTTP OpenCode 服务。
- 会话复用原生 session ID；CLI 重启后使用 session/load 恢复上下文。定时任务仍使用独立会话。取消/配置修改取消活动运行。会话删除通过原生 CLI session delete 执行。

## 网页供应商与模型配置

- 设置页读写专用工作区的原生 `opencode.json`。支持 JSON 编辑以及添加兼容 OpenAI 供应商模板，包含 provider/npm/options/baseURL/apiKey/models。允许 provider、model、small_model；其余代理权限、插件、MCP、命令和工作区控制由面板负责，不允许配置覆盖。
- 管理员配置编辑器直接读取和显示包括密钥在内的完整配置（用户明确要求不脱敏）；接口 no-store，文件权限 0600，不记录 SQL/日志，错误文本过滤密钥。拒绝文件/环境变量插值，避免误读宿主机凭据。
- 配置大小上限 128 KiB、原子写入、写入前验证 JSON 形状及 provider/npm；配置保存取消当前运行并刷新原生 CLI 模型列表。模型列表由 `opencode models` 获取，只展示所配置供应商的模型。
- CLI 使用面板专属配置/数据/缓存目录，不读取用户全局 OpenCode 配置和供应商环境凭据。系统 TLS/网络代理设置可用于下载和模型访问。

## 工具与权限

面板启动仅监听 loopback 的 MCP Streamable HTTP 服务，以随机 bearer 保护；动态注册到专用 OpenCode 工作目录。提供有界、参数化、只读审计工具（保留策略、客户端/网站聚合、分页详情、最近一分钟速率、24 小时分钟历史），不暴露面板全库、凭据或任意写 SQL。工具调用有超时、最大行数、并发限制；支持创建自然语言任务，但定时执行期间禁止再创建任务。外部日志内容视为不可信数据。会话禁止 shell、文件写入及通用网络工具，使用面板专用 session 权限；敏感动作不通过模型执行。

## 对话与任务

对话发送后立即返回运行 ID，后台启动 CLI，页面通过 SSE 展示实时文字、思考和工具状态；刷新/重连先回放有界快照，生成内容不逐 token 入库；会话串行、全局有界并发。OpenCode 远端会话引用绑定本地会话，不能任意访问其他会话。支持新建、查看、删除会话和取消运行。历史由面板本地保存展示文本与工具执行摘要，完整推理上下文由 OpenCode 保管。

任务含名称、任意语言目标、模型、每隔 N 小时（1..8760，默认12）、是否发管理员 Telegram、启用状态。支持新增/编辑/删除、暂停与立即运行。对话工具重试创建同名、同目标、同周期且 Telegram 选项一致的启用任务时，返回已有任务，防止重复调度。调度使用现有面板 cron 每分钟检查；持久化下次执行时间、运行状态、成功结果、执行与通知错误。相同任务不重叠；重启时将遗留运行标记中断，不无限重试或重放错过周期。每次任务创建独立 OpenCode 会话，结束后释放远端上下文。Telegram 通过既有管理员 Bot 配置发送，仅发管理员；发送失败与分析失败分别显示，不向用户 Bot 广播。

## 接口与持久化

/panel/api/ai/settings GET/POST；/reconnect POST（取消当前运行并重新连接）；/status GET；/models GET；/sessions GET/POST；/sessions/:id GET；/sessions/:id/delete POST；/sessions/:id/messages POST；/runs GET（任务历史，可按 taskId 筛选）；/runs/:id GET；/runs/:id/cancel POST；/tasks GET/POST；/tasks/:id/delete POST；/tasks/:id/run POST。接口均为管理员身份及现有 CSRF；分页/数量/文本长度受限。

持久化模型 AIConversation、AITask、AIRun（设置存于 Setting.panelAIConfig） 在 allModels 与 migrationModels 同时注册。运行历史按设置保留天数清理并可删除会话，任务运行记录分页查询；不记录模型或 Telegram 密钥。

## 验收

无 OpenCode 时状态明确引导一键部署；已安装时使用保存的 CLI 设置，读取真实模型，完成对话并实际调用审计工具；任务自然语言创建、到期运行、失败记录及管理员通知；取消释放运行；重启恢复不重放；越权和非法参数被拒绝；SQLite/PG 模型一致；导出和 AI 查询不会逐行查数据库或把整个日志库加载入上下文。


## CLI 接口补充与验收

- `GET/POST /deployment`：查看/启动安装任务，管理员认证与 CSRF。安装任务随面板关闭取消，失败不切换设置。
- `GET/POST /provider-config`：完整读取/原子保存 OpenCode 配置，响应包含 config JSON 文本与文件路径。
- `GET /runs/:id/events`：管理员认证、no-store、禁代理缓冲的 SSE；状态快照包含文字、思考、工具、错误与结束状态，原始协议事件及密钥不向浏览器透传。
- 验收场景：全新机器按钮安装及自动配置；下载/校验失败保留原版本；自定义供应商保存、完整密钥显示/修改/删除；CLI 首轮、续聊、工具调用、定时任务、取消/超时；增量显示、重连恢复与分页历史；旧 HTTP 设置迁移后不再使用 HTTP。

## 对话剩余高度（2026-10-19）

AI 页面占据动态视口高度，扣除提示条和页签栏后，官方 OpenCode iframe 必须占满全部剩余高度。按当前 Ant Design 6 的 body-holder → body → content 结构逐层使用可收缩的 flex 布局，不依赖旧版 content-holder/tabpane 类名或固定高度差值。窗口尺寸变化时自动适配；任务与设置内容在各自页签内部滚动，隐藏页签不得参与高度分配。
