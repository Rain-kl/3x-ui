# 2026-11-12 上游同步

面板从本地 `66e3b1c7` 合并 `74c3cc38..e535df03`，242 个提交。原目录含 `.zcodeignore`、`Xray-core/`、`db/` 未跟踪内容，因此在独立工作区完成同步，最后快进原分支，不推送。

## 用户确认的语义

保留独立用户 Bot（绑定码、重置、通知、续费、聊天），合入上游管理员 Bot 的可编辑菜单。完整采用 Host Mihomo X25519 的订阅生成行为和旧开关一次性清空。定制核心也同步上游并另行本地提交，核心发布另行处理。

## 冲突处理

- 广告实现维持删除；上游新导航清单去掉 Sponsors 注册，在原导航及公开 API 注册文件保留 `FORK(audit-workbench)`。移除守卫同时扫描新增生产文件和全部 locale，已以重新引入上游导航的方式验证失败，再验证合并版本通过。
- 路由、侧边栏、命令面板、标题统一采用上游集中清单；fork 页面与关键词迁到 `forkPanelRoutes.tsx`，减少四处重复接线。路由拖拽采用上游共享 hook，保留权威同步组和本地前置组的边界限制。
- 上游逐客户端身份批量读取结合 fork 的失败拒绝、原始入站明细和错误上下文；原始明细查询按 SQL 参数上限分批。重复流量快照不写空增量，避免静默恢复全量更新。
- 配额启停在独立文件组合上游启停方法，返回实际变更身份以生成 Runtime 计划；保留手动停用，原始明细与计费总量仍分开。
- 核心更新继续使用 Rain-kl 的最新 feat，结合上游预下载、候选配置检查及同文件系统原子替换；保留 SHA256 校验。Docker 平台变量统一采用上游新名称，避免 arm/386 下载与二进制名错配。
- API/类型/MDX 从合并后定义重新生成；包管理保留 pnpm，接受上游删除未用 MSW 浏览器 worker，重新生成锁文件。新增 Slovak locale 补齐 fork 英文键。
- 上游移除 `useZodForm` 后，fork 页面直接使用 RHF 与 zodResolver；接受新无障碍规则并补齐控件标签和装饰图标属性。
- 上游测试按 fork 契约调整：逐入站原始计数、内部计费身份、最新 feat 限制、路由脚本拒绝、已迁移客户端夹具。分页测试点击实际可访问控件，并保持重渲染时的相同 provider 树。AI 定时任务夹具使用 UTC，避免本地时区的 SQLite 时间文本比较错位。

## 配套核心模块

核心提交 `07fdd84a92b83ac3c1af5d11af16eaccb7088362` 同步至上游 `c4561418`（87 个提交），保留限速、嗅探审计、mixed 和纯 X25519 兼容。面板固定对应模块版本 `v0.0.0-20261112021401-07fdd84a92b8`。通过 `git archive` 导出纯提交树并用 `golang.org/x/mod/zip` 生成本地模块代理，避免测试产物污染 h1；`go mod verify` 通过。该提交尚未推送，其他机器在核心提交可获取前需使用相同本地模块代理或 Go workspace；不得先发布面板。

## 检查入口

仓库没有 `pnpm architecture:check` 或 `pnpm fork:check-removals` 脚本。实际运行移除脚本 `node scripts/check-fork-removals.mjs`，结构契约由 Go 的路由登记、主从接口、模型双迁移清单测试覆盖；不为消除命令缺失而伪造检查。验证结果见下节。

## 初始冲突文件

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `DockerInit.sh`
- `docs/content/docs/en/reference/api/authentication.mdx`
- `docs/content/docs/en/reference/api/clients.mdx`
- `docs/content/docs/en/reference/api/hosts.mdx`
- `docs/content/docs/en/reference/api/inbounds.mdx`
- `docs/content/docs/en/reference/api/nodes.mdx`
- `docs/content/docs/en/reference/api/server.mdx`
- `docs/content/docs/en/reference/api/settings.mdx`
- `docs/public/openapi.json`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/public/mockServiceWorker.js`
- `frontend/public/openapi.json`
- `frontend/src/components/command-palette/CommandPalette.tsx`
- `frontend/src/components/sponsor/SponsorCard.css`
- `frontend/src/components/sponsor/SponsorCard.stories.tsx`
- `frontend/src/components/sponsor/SponsorCard.tsx`
- `frontend/src/components/sponsor/SponsorSlot.tsx`
- `frontend/src/generated/examples.ts`
- `frontend/src/generated/schemas.ts`
- `frontend/src/generated/types.ts`
- `frontend/src/generated/zod.ts`
- `frontend/src/hooks/usePageTitle.ts`
- `frontend/src/layouts/AppSidebar.tsx`
- `frontend/src/lib/sponsors.ts`
- `frontend/src/pages/clients/ClientFormModal.tsx`
- `frontend/src/pages/inbounds/form/protocols/mixed.tsx`
- `frontend/src/pages/nodes/NodeList.tsx`
- `frontend/src/pages/sponsors/SponsorsPage.css`
- `frontend/src/pages/sponsors/SponsorsPage.tsx`
- `frontend/src/pages/xray/routing/RoutingTab.tsx`
- `frontend/src/routes.tsx`
- `frontend/src/test/sponsors.test.tsx`
- `go.mod`
- `go.sum`
- `internal/database/db.go`
- `internal/database/model/model.go`
- `internal/sub/clash_service.go`
- `internal/sub/controller.go`
- `internal/sub/json_service.go`
- `internal/sub/service.go`
- `internal/web/runtime/local.go`
- `internal/web/runtime/remote.go`
- `internal/web/runtime/remote_test.go`
- `internal/web/service/client_crud.go`
- `internal/web/service/client_link.go`
- `internal/web/service/inbound.go`
- `internal/web/service/inbound_clients.go`
- `internal/web/service/inbound_disable.go`
- `internal/web/service/inbound_node.go`
- `internal/web/service/inbound_traffic.go`
- `internal/web/service/panel/panel.go`
- `internal/web/service/panel/sponsor.go`
- `internal/web/service/panel/sponsor_test.go`
- `internal/web/service/server.go`
- `internal/web/service/tgbot/tgbot_add_client_picker_test.go`
- `internal/web/service/tgbot/tgbot_broadcast.go`
- `internal/web/service/tgbot/tgbot_inbound.go`
- `internal/web/service/tgbot/tgbot_send.go`
- `internal/web/service/xray.go`
- `internal/web/service/xray_setting.go`
- `internal/web/translation/ar-EG.json`
- `internal/web/translation/en-US.json`
- `internal/web/translation/es-ES.json`
- `internal/web/translation/fa-IR.json`
- `internal/web/translation/id-ID.json`
- `internal/web/translation/ja-JP.json`
- `internal/web/translation/pt-BR.json`
- `internal/web/translation/ru-RU.json`
- `internal/web/translation/tr-TR.json`
- `internal/web/translation/uk-UA.json`
- `internal/web/translation/vi-VN.json`
- `internal/web/translation/zh-CN.json`
- `internal/web/translation/zh-TW.json`
- `internal/web/web.go`
- `internal/xray/hot_diff.go`
- `update.sh`

## 最终验证

- `make test-go` 全部包通过（随机顺序、无缓存），包含路由、主从接口、模型双迁移清单及全部 fork 功能回归。PostgreSQL 和专用环境测试遵循仓库环境变量条件；本次使用 SQLite。
- 前端 unit/components 共 209 个文件、2084 项测试通过；安装升级后 Playwright 所需 Chromium 后，31 个 Storybook 文件、100 项真实浏览器测试通过，总计 2184 项。
- `XRAY_E2E_BINARY=/tmp/xray-core-sync-20261112 go test ./internal/xray -run E2E -count=1` 通过。配套核心的限速/审计/mixed/协议/配置及 trafficlog 测试、关键包 race 检查、protobuf 生成契约与二进制构建通过。
- `make gen-check`、Go lint（0 issues）、前端 lint、格式检查、类型检查、Vite/Go/Storybook 构建通过。`node scripts/check-ci-config.mjs` 与移除守卫通过；模块校验通过。验证门的全部目标分步执行。
- 广告导航和 managed mixed 选择器的回归检查均已观察到移除对应修复后失败、恢复后通过。

初轮完整测试发现并修正 SQL 批量上限、手动禁用复活和空增量写入；其余上游断言与夹具依照本 fork 的原始计量、内部身份和定制版本契约对齐。AI 测试夹具使用 UTC 后通过，生产调度逻辑不变。
