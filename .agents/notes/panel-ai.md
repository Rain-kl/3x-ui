# 本地 OpenCode 面板助手

## CLI、托管部署与实时对话（2026-10-19）

本节取代上文 HTTP Server API 与手动服务连接方案。用户要求 CLI 通信、一键安装、网页维护原生模型配置及实时输出。使用 OpenCode 原生 ACP stdio JSON-RPC，不实现 agent 循环。按运行启动 `opencode acp --pure`，session/load 续聊，原生 session/set_model 选模型；session/update 的文字、思考与工具进度经有界内存快照及认证 SSE 转发，历史仍最终入库。加载历史时不接收旧消息增量，防止重复显示。原生工具权限默认拒绝，只开放审计 MCP；定时运行禁止创建任务。审计 MCP 仍是本机 loopback 工具传输，不是 OpenCode HTTP 对话接入。

一键安装固定官方 1.19.3 独立包、内置校验和、限定平台与最大大小，仅提取目标常规可执行文件；版本检查成功再切换绝对路径。下载失败保留现有安装；同版本托管程序再次点击复用，不重复堆积文件。子进程使用专属 XDG/home/config，过滤继承的供应商密钥与 OpenCode 配置变量。Linux/macOS 使用进程组取消拥有的进程树；Windows 使用直接进程取消。

设置页 JSON 编辑器支持 provider/model/small_model 和兼容供应商模板，运行时权限/工具/MCP 不可覆盖。用户明确要求管理员查看完整配置，因此 API 返回完整 JSON，不设脱敏占位符；no-store 与原子 0600 文件写入保留，异常日志仍过滤配置密钥。不再使用旧 URL、账号、密码；保存新设置丢弃旧字段。配置变化取消运行。原生 models 输出只展示配置的供应商，ACP enabled_providers 同样限制到它们，避免空配置意外使用内置免费模型。

SSE 绕开 gzip 包装并解除正常30秒写超时，每次写单独10秒超时、15秒心跳、100ms合并快照；刷新/重连回放当前状态，不逐token写数据库。前端100ms合并渲染，结束刷新历史。既有确定性 HTTP fixture 已迁移为 CLI 子进程 fixture；真实集成入口改用 XUI_TEST_OPENCODE_BIN、XUI_TEST_OPENCODE_CONFIG 与可选模型。此次未新增测试用例或执行测试，仅进行生成、静态检查与构建。

## 历史方案（以下 HTTP 接入已被 CLI 方案取代）

## 选型与边界

用户确认以 OpenCode Server API 代替未确认可用的 A2A 协议。`internal/panelai` 只负责持久化引用、请求转发、调度和 MCP 适配；原生 OpenCode 执行模型、上下文和工具循环。使用专用工作目录和受限 session：默认拒绝权限，关闭其他工具，只开放面板审计工具；任务执行另外禁止创建任务。保持 OpenCode 原生系统提示，业务目标作为消息上下文交给它。

审计工具经 `service.PanelAIHooks` 查询保留策略、分页客户端/网站、详细日志及分钟速率，不提供任意 SQL、账号表或写入能力。MCP 仅监听 loopback，随机 bearer，不挂到公网 Web 路由。没有 provider 密钥复制到面板；`/provider` 只返回已连接模型的 ID/名称。只连接本地 URL；没有服务时通过手动设置的绝对可执行文件路径启动自己拥有的进程，认证错误不尝试抢占端口，关闭时不停止外部服务。

## 状态和调度

三个独立模型：会话、任务、运行。设置保存在既有 Setting。普通会话复用 OpenCode session，任务每次创建并释放独立 session。全局最多两个运行，同一会话/任务不可重叠；任务事务锁定并更新 nextRun，重启把残留运行标记 interrupted，不重放已经领取的周期。取消请求向原生 session 发送 abort；运行结果、工具状态、分析错误和 Telegram 发送错误分别保存。旧运行按独立保留天数清理，删除会话时删除远端上下文。

Telegram 复用管理员配置、代理和 API 服务，通过既有用户 Bot 模块的 transport 发送纯文本并分段；不启用额外 update receiver，不向用户 Bot 广播。没有管理员配置时记录真实失败。

## 本地环境与检查

本次工作环境安装 OpenCode 1.19.3 到 `/workspace/work/opencode`，独立 XDG/config/data/cache。免费模型在受限 Server API 会话中返回访问限制，因此另配官方 llama.cpp CPU 服务和 Qwen3 1.7B（Q4_K_M）作为无外部凭据的链路验证模型。该小模型用于确认调用链路，实际审计应选择有足够分析和工具能力的模型。

真实 OpenCode 已验证审计工具调用和实际创建12小时 Telegram 任务（本地测试库，不对外发消息）。真实 OpenCode 检查需设置 `XUI_TEST_OPENCODE_URL` 和可选 `XUI_TEST_OPENCODE_MODEL`，默认 CI 跳过，以免依赖外部服务/产生费用。确定性检查覆盖 MCP 调用、任务到期去重、权限限制、取消及通知错误；审计查询检查覆盖分页和客户端隔离。现有模型迁移注册与路由注册契约同时检查。

## 手动连接配置更新

连接参数以 AI 设置为唯一来源：服务地址、绝对可执行路径、绝对工作目录、认证账号和密码。禁用环境/目录/PATH/默认端口发现；目录留空仅选择面板自己的 ai 工作区，不扫描本地服务。程序路径为空时不会启动进程。模型仍来自 OpenCode 原生模型接口。

私有 readSettings 保留认证密码，公开 ReadSettings 清空原文并返回 hasPassword；保存响应重新读取脱敏视图，不回显请求密码。保存 SQL 使用静默 logger，HTTP/启动错误脱敏；空白密码保留旧值。子进程认证只取已保存值，连接字段变化重建连接并取消任务。
