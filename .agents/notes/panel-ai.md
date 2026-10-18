# 本地 OpenCode 面板助手

## 选型与边界

用户确认以 OpenCode Server API 代替未确认可用的 A2A 协议。`internal/panelai` 只负责持久化引用、请求转发、调度和 MCP 适配；原生 OpenCode 执行模型、上下文和工具循环。使用专用工作目录和受限 session：默认拒绝权限，关闭其他工具，只开放面板审计工具；任务执行另外禁止创建任务。保持 OpenCode 原生系统提示，业务目标作为消息上下文交给它。

审计工具经 `service.PanelAIHooks` 查询保留策略、分页客户端/网站、详细日志及分钟速率，不提供任意 SQL、账号表或写入能力。MCP 仅监听 loopback，随机 bearer，不挂到公网 Web 路由。没有 provider 密钥复制到面板；`/provider` 只返回已连接模型的 ID/名称。只连接本地 URL；没有服务时通过发现的二进制启动自己拥有的进程，认证错误不尝试抢占端口，关闭时不停止外部服务。

## 状态和调度

三个独立模型：会话、任务、运行。设置保存在既有 Setting。普通会话复用 OpenCode session，任务每次创建并释放独立 session。全局最多两个运行，同一会话/任务不可重叠；任务事务锁定并更新 nextRun，重启把残留运行标记 interrupted，不重放已经领取的周期。取消请求向原生 session 发送 abort；运行结果、工具状态、分析错误和 Telegram 发送错误分别保存。旧运行按独立保留天数清理，删除会话时删除远端上下文。

Telegram 复用管理员配置、代理和 API 服务，通过既有用户 Bot 模块的 transport 发送纯文本并分段；不启用额外 update receiver，不向用户 Bot 广播。没有管理员配置时记录真实失败。

## 本地环境与检查

本次工作环境安装 OpenCode 1.19.3 到 `/workspace/work/opencode`，独立 XDG/config/data/cache。免费模型在受限 Server API 会话中返回访问限制，因此另配官方 llama.cpp CPU 服务和 Qwen3 1.7B（Q4_K_M）作为无外部凭据的链路验证模型。该小模型用于确认调用链路，实际审计应选择有足够分析和工具能力的模型。

真实 OpenCode 已验证审计工具调用和实际创建12小时 Telegram 任务（本地测试库，不对外发消息）。真实 OpenCode 检查需设置 `XUI_TEST_OPENCODE_URL` 和可选 `XUI_TEST_OPENCODE_MODEL`，默认 CI 跳过，以免依赖外部服务/产生费用。确定性检查覆盖 MCP 调用、任务到期去重、权限限制、取消及通知错误；审计查询检查覆盖分页和客户端隔离。现有模型迁移注册与路由注册契约同时检查。
