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
