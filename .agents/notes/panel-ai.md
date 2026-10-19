## SQL 聚合审计（2026-10-19）

删除 agent 可见的分页 query，改为 execute_sql 单条 SELECT/WITH DQL，保留 create_task。官方 Web agent system、ACP 每次请求与 MCP initialize.instructions 都注入同一环境说明：真实方言、UTC快照、保留策略、六张日志表的全部字段语义、秒/毫秒差异、NULL计量、归档互斥、分钟速率和带宽覆盖。提示词先全客户端SQL聚合和排名，再取少量候选明细，禁止 SELECT * / 逐页整库读取。系统时间标注为启动快照，滚动窗口由数据库 CURRENT_TIMESTAMP 计算。

SQL分析沿用2个审计并发槽和30秒上下文；200行、64列、256KiB及单字段4096字节上限，结果含columns/rows/truncated/asOf。SQLite新建短生命周期 mode=ro/query_only 连接，SQLite原生authorizer只允许六张日志表，关闭连接释放回调句柄，不污染业务连接池，也避免反复注册authorizer的句柄积累。PostgreSQL使用原生PostgreSQL语法树（pg_query）验证单条SELECT、读表、CTE、函数和类型，禁写CTE/INTO/锁定及自定义或volatile函数；另检查原生 EXPLAIN 实际计划中的基表，避免同名嵌套 CTE 的作用域掩盖非日志表；再在只读事务中执行。数据库执行聚合可能扫描索引/数据，这是服务端计算；不等同于把原始全表传给模型。

连接计数不是并发；audit_receipts仅防重；日归档可能来自延迟子节点，不能机械以详细保留边界裁掉近期汇总。带宽client=''行仅记录覆盖，客户端行covered_seconds通常为0；按epoch/minute/node先聚合再连接覆盖，避免乘法放大。保留现有确定性检查并随工具协议迁移fixture，不新增测试用例，不向真实模型/Telegram发请求。

## OpenCode v2 官方 Web（2026-10-19）

固定官方 @opencode/cli 2.0.32；v2 npm 包名/配置结构/CLI 参数均与 v1 不同，不能只改版本字符串。安装从官方 npm 下载按平台的独立二进制，固定发布摘要校验 SHA-512，宿主机不依赖 Node。官方 Web 源码由 scripts/build-opencode-web.py 固定版本和源包 SHA-256 重建，仅调整 hash 路由、服务地址、同源写请求 CSRF 和禁用 service worker；完整产物压缩入面板，附 MIT 许可证。不要对打包混淆 JS 做字符串补丁。HTML先重写官方根资源路径，再注入base/meta，避免新base被重复拼接面板前缀。

上游 `currentRoute` 等代码直接按根路径解析 `useLocation().pathname`，官方 `Router` 的 `base` 只给路由模式加前缀，不会从 pathname 去掉部署前缀；面板挂在随机路径下时解析得到空结果并触发错误页。适配改用官方 `HashRouter`，页面 URL 形如 `<panel>/panel/ai/opencode/#/...`，让全部 pathname 读取和前载看到根相对路径；`panel-opencode-base` 仍只用于服务地址与 CSRF 请求。`officialweb` 的回归测试读取入口脚本并断言存在 `hashchange`，重建时漏掉适配会直接失败。

Web 服务使用 serve --stdio 的拥有者 stdin lease，由 readiness JSON 读取真实随机 loopback 端口，随机密码只存内存并经服务端注入 BasicAuth。官方页面和全部代理 API 单独逐请求检查真实管理员会话、Origin 和 CSRF，不走 API Token 的豁免路径；拒绝 auth/pair 入口，不代理浏览器 Cookie/Authorization、不转发服务 Set-Cookie/认证挑战/重定向。普通服务仅监听 127.0.0.1，设置变化及退出关闭整个进程组。仅流式 API 代理禁 gzip、清除正常写超时并立即 flush，支持 WS 升级；官方静态脚本/WASM 沿用 gzip（官方全部 JS 约29MiB、WASM约34MiB，不能将整个界面前缀排除压缩）。

专用配置规范化 providers/package/settings；旧 provider/npm/options 保留大小写后迁移，不能把 display name 当 ID。v2 --version 输出带 opencode v 前缀，acp 不支持 --pure/--cwd，models 和 session delete 使用 --standalone。供应商保存重启官方服务并取消任务，原生服务及任务共享隔离 XDG 数据目录与审计 MCP。

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

## Ant Design 6 remaining-height fix

The previous layout styled obsolete ant-tabs-content-holder / ant-tabs-tabpane
classes. Installed @rc-component/tabs 1.13 renders ant-tabs-body-holder >
ant-tabs-body > ant-tabs-content. The intermediate body remained auto-sized,
so the iframe's percentage height did not receive the remaining viewport space.
Make all three current containers flex columns with flex:1/min-height:0 and
exclude hidden content panes. Let the iframe flex from a zero height instead
of an unresolved percentage; task/settings bodies retain internal scrolling.
Use 100dvh (100vh fallback) to track mobile viewport changes as well.

Height fix: formatted the affected CSS and rebuilt the production frontend
successfully. No automated test suite or full pre-push gate was run, following
the user's prior instruction to skip push validation. Browser layout has not
been verified against the user's running deployment.

## ACP v2 model selection

OpenCode 2.0.32 registers session/set_config_option and no session/set_model.
Its model selector takes configId=model and value=providerID/modelID. Replace
the legacy call without fallback or swallowing invalid-model errors. The
provided providers/package/settings JSON matches v2 schema and the compatible
adapter accepts settings.apiKey/baseURL. Top-level model is optional when the
task explicitly selects its model; provider display name is not its ID.
Strengthen the existing fixture: reject unknown RPC methods and require model
selection before prompting, with an explicit fixture/model default in the
existing scheduled-run/cancellation scenario.

Validation: existing TestScheduledOpenCodeRunAndCancellation passed (0.168s).
The real @opencode/cli-linux-x64-baseline 2.0.32 binary accepted the same v2
provider structure with placeholder credentials and a loopback endpoint,
created an ACP session, and returned currentValue=arctel/flash after
session/set_config_option. No session/prompt or external provider request
was sent. Full push checks remain skipped per the earlier user instruction.

## Delete execution records

Add a manager-owned deletion operation under runMu, checking both the active
worker map and persisted running state. Delete only AIRun and its in-memory
stream, leaving tasks, schedules and native conversation sessions intact.
The history cards confirm deletion, disable it for running records, expose
errors and move back one page when deleting the last item on a non-first page.
Reuse translated delete/confirmation keys; optional RunResult props preserve
the legacy conversation component. Register the new authenticated POST route
in endpoint metadata and regenerate frontend/docs OpenAPI outputs.
Per explicit user instruction: no checks, tests, build or Git hooks; commit
only, with no push requested for this change.
