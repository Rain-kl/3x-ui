# 面板 AI：复用本地 OpenCode

## 架构与所有权

用户已确认采用 OpenCode 原生 Server API。面板不实现 agent 循环、模型提供商或推理运行时：OpenCode 持有模型配置、会话消息、工具调用和上下文；面板持有连接设置、会话引用、自然语言定时任务及运行结果。侧边栏 AI 进入「对话、任务、设置」。模型列表来自 GET /provider 的已连接 provider，不读取或返回其凭据；选择默认模型，provider 的密钥与连接仍在 OpenCode 管理。

## 自动发现与生命周期

优先显式设置 URL，然后 XUI_OPENCODE_URL / OPENCODE_SERVER_URL，然后本地配置中的 server 端口和默认 127.0.0.1:4096。二进制发现 XUI_OPENCODE_BIN / OPENCODE_BIN、PATH、用户 .opencode/bin / .local/bin、面板目录 node_modules/.bin。工作目录为面板 DB 目录下的独立 ai 工作区，可用 XUI_OPENCODE_DIRECTORY 指定。仅启用 AI 时连接或启动发现的本地二进制；面板只停止自己启动的进程。服务认证使用 OPENCODE_SERVER_PASSWORD / OPENCODE_SERVER_USERNAME。状态如实报告路径、连接错误，禁止静默回退到伪造 AI 结果。

## 工具与权限

面板启动仅监听 loopback 的 MCP Streamable HTTP 服务，以随机 bearer 保护；动态注册到专用 OpenCode 工作目录。提供有界、参数化、只读审计工具（保留策略、客户端/网站聚合、分页详情、最近一分钟速率、24 小时分钟历史），不暴露面板全库、凭据或任意写 SQL。工具调用有超时、最大行数、并发限制；支持创建自然语言任务，但定时执行期间禁止再创建任务。外部日志内容视为不可信数据。会话禁止 shell、文件写入及通用网络工具，使用面板专用 session 权限；敏感动作不通过模型执行。

## 对话与任务

对话发送后立即返回运行 ID，后台请求 OpenCode，页面轮询结果；会话串行、全局有界并发。OpenCode 远端会话引用绑定本地会话，不能任意访问其他会话。支持新建、查看、删除会话和取消运行。历史由面板本地保存展示文本与工具执行摘要，完整推理上下文由 OpenCode 保管。

任务含名称、任意语言目标、模型、每隔 N 小时（1..8760，默认12）、是否发管理员 Telegram、启用状态。支持新增/编辑/删除、暂停与立即运行。对话工具重试创建同名、同目标、同周期且 Telegram 选项一致的启用任务时，返回已有任务，防止重复调度。调度使用现有面板 cron 每分钟检查；持久化下次执行时间、运行状态、成功结果、执行与通知错误。相同任务不重叠；重启时将遗留运行标记中断，不无限重试或重放错过周期。每次任务创建独立 OpenCode 会话，结束后释放远端上下文。Telegram 通过既有管理员 Bot 配置发送，仅发管理员；发送失败与分析失败分别显示，不向用户 Bot 广播。

## 接口与持久化

/panel/api/ai/settings GET/POST；/reconnect POST（取消当前运行并重新连接）；/status GET；/models GET；/sessions GET/POST；/sessions/:id GET；/sessions/:id/delete POST；/sessions/:id/messages POST；/runs GET（任务历史，可按 taskId 筛选）；/runs/:id GET；/runs/:id/cancel POST；/tasks GET/POST；/tasks/:id/delete POST；/tasks/:id/run POST。接口均为管理员身份及现有 CSRF；分页/数量/文本长度受限。

持久化模型 AIConversation、AITask、AIRun（设置存于 Setting.panelAIConfig） 在 allModels 与 migrationModels 同时注册。运行历史按设置保留天数清理并可删除会话，任务运行记录分页查询；不记录模型或 Telegram 密钥。

## 验收

无 OpenCode 时状态明确报错；有本地服务时自动发现，读取真实模型，完成对话并实际调用审计工具；任务自然语言创建、到期运行、失败记录及管理员通知；取消释放运行；重启恢复不重放；越权和非法参数被拒绝；SQLite/PG 模型一致；导出和 AI 查询不会逐行查数据库或把整个日志库加载入上下文。
