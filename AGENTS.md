# AGENTS.md

## 核心原则

- 新增或修改行为前，先更新对应 spec；目录不存在时按需创建。先明确产品规则、状态所有者、接口和验收场景，再实现代码。
- 定位问题时，未明确要求修改代码就先调查原因。结合源码、日志和运行时证据，区分已确认原因与待验证假设。
- 保留与任务无关的本地改动，不自行恢复已移除的模块或内部依赖。
- 代码实现前必须以架构师的视角在思维链反问自己, 这么做是否是最佳实现方式, 如果不合理或者存在更高的实现方案必须停止开发, 并将告知用户, 由用户进行决断

## 代码规范

1. 一个类或模块只做一件事，职责一旦膨胀就会变成改不动、测不了、拆不开的神类。
2. 优先组合而非继承，继承是把耦合写进类型层次里的最快方式。
3. 面向接口和抽象编程，调用方不该知道具体实现，否则替换和测试都会牵一发而动全身。
4. 把会变的部分封起来，把稳定的部分写短、写直、写死。
5. 先做出能跑、能测、能理解的最小实现，真正需要时再抽象，过早设计是屎山的温床。
6. 业务规则留在领域层，IO、框架、中间件和第三方全部推到边界之外。
7. 禁止将报错隐瞒包装, 将不同类型的错误抛出同一个错误提示结果, 可能产生的错误必须携带该点错误详细的上下文逐级抛出. 避免隐藏错误导致排查困难


## Git 提交规范

每次完成一个功能点开发或修复一个问题后，务必提交 Git commit , 禁止推送远程仓库。
遵循 Conventional Commits：`<type>(<scope>): <subject>`（例：`feat(auth): support email login`）。
若涉及非琐碎改动（技术选型/架构重构/核心改动/踩坑复盘），必须将对应的 Agent Note（`.agents/notes/...`）与代码同批原子提交。
- **改动范围约束**：仅允许提交自己明确修改的文件，严禁提交非本次任务修改的文件（杜绝盲目 `git add .` 或 `git commit -a` 卷入无关变动）。
- **工作区保护**：严禁随意还原、丢弃或覆盖他人或当前工作区正在施工的文件。
- **测试容错与告知**：若工作区正在施工的文件导致全局测试失败，仅验证本次修改相关的测试用例，跳过受无关施工影响的测试并明确告知跳过原因。

## 二开标准（Fork）

当前项目属于3x-ui 二开版本。核心约束：**把每次同步上游的冲突面压到最小，并让残留冲突可被机械定位**。以下规则对所有改动生效。

### 远端与同步

- 同步用 `git fetch upstream && git merge upstream/main`，不改写已推送的提交历史。`canary` 是集成分支，推送即触发集成构建。
- 同步前确认工作区干净，并在独立分支上解决冲突；同步后按「同步与冲突解决」收尾。

### 上游文件改动标记

改动上游既有**代码与配置文件**的每一处都必须留标记，仅限**产品类**改动（功能、行为、体验、文案）。`<feature-id>` 必须与 `FEATURES.md` 条目标题里的 id 一致（「其他更新」类的小改动同样用 `其他更新` 作为 id）：

```ts
// FORK(<feature-id>): 为什么必须改上游代码；见 FEATURES.md 的 <feature-id> 条目
```

连续多行改动用成对标记包住，便于同步时定位边界：

```ts
// FORK-BEGIN(<feature-id>)
// ... 本项目改动 ...
// FORK-END(<feature-id>)
```

- 注释语法随语言调整（`#`、`<!-- -->` 等），token 固定为 `FORK(<id>)` / `FORK-BEGIN(<id>)` / `FORK-END(<id>)`。
- 标记里写「为什么必须改上游」，不写「改了什么」。
- 新增文件、新增包不需要标记：它们不产生逐行冲突，只在与上游同名同路径时才会 add/add 冲突（用特征命名规避）；只有触碰上游既有文件才必须标记。
- 非产品改动（CI 与发布流水线、构建脚本、仓库规范、开发工具链、面向开发者的文档）不打标记、不登记 `FEATURES.md`：只要求改动最小成块，原因写进提交信息。
- 自查：`rg -n "FORK\(|FORK-BEGIN\(" --glob '!AGENTS.md' --glob '!FEATURES.md'`。

### 文档与 FEATURES.md

`FEATURES.md` 只登记**产品本身**的二开改动（面向用户的功能、行为、体验、文案）。发布流水线、构建脚本、仓库规范、开发工具链、开发者文档等非产品改动不登记，用提交信息记录，避免变成流水账。每条记录包含：需求背景、修改内容、修改文件、上游改动标记、该功能的设计文档与实现文档链接。格式：

- **大功能需求**：独立 `##` 标题，格式 `## <功能名> (<feature-id>)`（id 供标记引用与检索），详细记录。
- **功能增强**：以 `###` 子标题放在对应大功能标题下方，写明增强点与影响的上游标记。
- **产品细节/文案等小改动**：统一写在文末 `## 其他更新`，一行一条（日期 + 简述 + 文件），不单独建设计文档；非产品改动不写进本文件。

### 同步与冲突解决

操作手册见 `MERGE_GUIDE.md`（预演冲突面、去重流程、收尾清单、故障速查）。以下是规则层面不可跳步的顺序：

1. **先外挂**：冲突能靠把改动迁到 fork 包/目录消除时，先迁移，再删掉上游文件里的那段改动。
2. **再贴合**：无法外挂时，以上游最新实现为基线，把我们的改动以最小 diff 重新贴回，并保持标记完整。
3. **再对齐**：上游新实现与二开语义互斥（同一行为、不同方案）时，不机械合并。产品类冲突在 `FEATURES.md` 记录「同步冲突」条目（上游提交、冲突文件、两种语义取舍）；非产品类冲突把取舍写进合并提交信息。与用户确认后再改。

禁止：整文件锁定上游（`-X ours`、`.gitattributes merge=ours`、跳过上游提交）、为保住旧实现删掉上游新逻辑、用 `--force` 覆盖他人分支。

同步完成后必须执行：`pnpm typecheck`、`pnpm lint`、`pnpm architecture:check`、`pnpm fork:check-removals`，以及受影响功能的验收场景；并把同步日期、上游提交范围、冲突文件与处理方式记入对应产品条目的「上游同步记录」，非产品类冲突记在合并提交信息里。

**为什么必须有 `pnpm fork:check-removals`**：二开删掉的文件/符号，merge 不一定会提醒你。实测四类情况：

- 上游没碰被删文件 → 删除保留，无冲突（文件不会回来）；
- 上游改了被删文件 → `CONFLICT (modify/delete)`，git 把上游版本留在工作区。**这里最现实的失误是把冲突按「保留上游版本」解决，等于撤销了删除**；
- 上游用**新文件**重新实现同一能力 → 0 冲突，完全静默；
- 删掉的是文件里的符号、上游在**别的文件**新增调用 → 0 冲突，静默，且只有该文件在类型检查范围内才可能被发现。

所以守卫断言的是**行为不变量**（被移除的符号/端点必须缺席 + 我们那版接线必须在位），而不是路径墓碑。规则表在 `scripts/check-fork-removals.mjs`，新增二开移除项时同步登记，并写进对应 FEATURES.md 条目。

Operational guide for AI agents working in this repo. Long-form human docs:
`CONTRIBUTING.md` (setup, testing philosophy) and `frontend/README.md`.
Read those before large changes. This file is the short, must-follow version.
For a deep navigation map (request lifecycle, cron-job table, symptom → file
index, layering rules), read `docs/architecture.md` on demand — do not guess
file locations when it can answer in one hop.

## Stack
- Backend: Go 1.27 (`module github.com/mhsanaei/3x-ui/v3`), Gin, GORM.
  Runs Xray-core as a managed child process (`internal/xray/process.go`) and
  imports `github.com/xtls/xray-core` for config types + gRPC stats/handler/router
  API. MTProto inbounds run a second managed child — the `mtg-multi` binary
  (a multi-secret mtg fork — NOT a Go dependency; its prebuilt release binary is
  fetched at image/release build time by `DockerInit.sh` + `release.yml`,
  panel-side code in `internal/mtproto/`) — outside Xray, one process per inbound
  serving each
  client's FakeTLS secret via the fork's `[secrets]` section (plus per-client
  ad-tags via `[secret-ad-tags]` and per-client data quota / expiry via
  `[secret-limits]`, mapped from the client's `totalGB`/`expiryTime`). Client,
  ad-tag and quota/expiry edits are hot-applied through the fork's management API
  (`PUT /secrets`, bearer-token guarded) so connections survive; the manager
  falls back to a process restart on older binaries. A client's panel-side
  traffic reset also calls `POST /secrets/{name}/reset-quota` so a renewed client
  is not re-blocked by the sidecar's quota counter.
- Storage: SQLite by default (`/etc/x-ui/x-ui.db` on Linux; the executable dir on
  Windows), PostgreSQL optional (`XUI_DB_TYPE` / `XUI_DB_DSN`). The CGo SQLite
  driver (`mattn/go-sqlite3`) needs a C compiler — `CGO_ENABLED=0` builds fail.
- Frontend: React 19 + Ant Design 6 + Vite 8 + TypeScript in `frontend/`,
  built into `internal/web/dist/` (gitignored) and embedded via `embed.FS`.

## Repo map
- `main.go` — entry point + `x-ui` CLI (run, migrate, migrate-db, encrypt-tokens, setting, cert).
- `internal/config/` — env parsing (XUI_DEBUG, XUI_LOG_LEVEL, XUI_LOG_FOLDER,
  XUI_BIN_FOLDER, XUI_SKIP_HSTS, XUI_PORT, XUI_DB_*).
- `internal/database/` + `internal/database/model/` — GORM schema (~24 models;
  Inbound, Client, Setting, User are the core), inbound Protocol enum,
  AutoMigrate + hand-written migrations in `db.go`.
- `internal/xray/` — Xray child-process lifecycle, config generation, gRPC API.
- `internal/xray/geodata/` — streaming geosite/geoip `.dat` reader (cached
  category index + paged entries) and `geosite:`/`geoip:`/`ext:` token parsing.
- `internal/mtproto/` — MTProto inbounds via the bundled `mtg-multi` binary.
- `internal/tuic/` — TUIC v5 inbounds: `tuic-server` sidecar supervisor, native Go UDP relay traffic metering.
- `internal/amneziawg/` — AmneziaWG protocol shape: instance/peer derivation
  from an inbound, 3.1 obfuscation param generation + validation, port-forward
  spec parsing.
- `internal/amneziawgnet/` — embedded AmneziaWG runtime: amneziawg-go device
  over a gVisor userspace netstack, per-inbound reconcile manager, TCP/UDP
  relay into a loopback per-peer-auth SOCKS5 Xray inbound, port-forward
  listeners, per-peer IPv6 egress aliases.
- `internal/pia/` — PIA WireGuard protocol client (auth, signed server list, `/addKey`).
- `internal/sub/` — subscription server (raw / JSON / Clash).
- `internal/eventbus/` — in-process pub/sub (outbound/node health, xray.crash,
  cpu.high, memory.high, login.attempt).
- `internal/logger/`, `internal/util/` (link, crypto, sys, ldap, …),
  `internal/tunnelmonitor/` — shared infrastructure.
- `internal/web/` — Gin server (embeds `dist/` + `translation/`).
  - `controller/` — panel + REST API handlers; OpenAPI at /panel/api/openapi.json.
  - `service/` — business logic (InboundService, SettingService, XrayService,
    node sync); subpackages tgbot/, discord/, email/, outbound/, panel/, integration/.
  - `job/` — 19 cron jobs (traffic, fail2ban IP-limit, node heartbeat/sync, LDAP,
    CPU/memory watchdogs, …); full table in `docs/architecture.md` §5.4.
  - `middleware/`, `entity/`, `global/`, `session/` (CSRF), `network/`,
    `runtime/` (master/sub-node over mTLS), `websocket/`.
  - `locale/` + `translation/` — i18n, 13 embedded locale JSON files.
- `frontend/` — React + TS source (see `frontend/CLAUDE.md`).
- `tools/openapigen/` — Go generator that emits frontend types + Zod/JSON schemas
  into `frontend/src/generated/` from Go structs. The OpenAPI doc itself
  (`frontend/public/openapi.json`) is assembled from those + `endpoints.ts` by
  `frontend/scripts/build-openapi.mjs`.
- `docs/` — separate Next.js/Fumadocs site (pnpm, own CI in `docs-ci.yml`,
  outside `make verify`). Holds a THIRD independent implementation of
  link/subscription generation in `docs/lib/xray/` — check it whenever
  share-link or install-command output changes.

## Hard rules (non-negotiable)
- Fix size must match bug size. Find the root cause, then make the SMALLEST
  change that removes it — a one-line guard beats a new subsystem. A small bug
  does not earn new columns, jobs, abstractions, config knobs or helper layers.
  If a fix genuinely needs new architecture, say so and get agreement first;
  never ship it unasked next to the fix.
- Comments in committed Go/TS: 2 lines MAX per comment block. Make the name
  carry the meaning first and rename rather than annotate; spend the 2 lines on
  the *why* a name cannot hold — an invariant, an issue number, a non-obvious
  constraint. Exempt: `//go:build`, `//go:generate`, and other directives.
  HTML `<!-- -->` is fine. (A linter cannot enforce this — you must.)
- New `g.POST`/`g.GET` in `internal/web/controller/` REQUIRES a matching entry
  in `frontend/src/pages/api-docs/endpoints.ts`, then `make gen` (or
  `cd frontend && npm run gen`). Hand-maintained but pinned both ways by
  `TestRouteRegistryContract` (`internal/web/routes_contract_test.go`): a missing
  OR stale entry fails `make test-go`. Scope: `/panel/api/*` + a few session
  routes; sub-server routes are exempt.
- Response examples come from Go struct `example:` tags via `tools/openapigen` —
  never hand-write them. A new struct must be added to openapigen's `StructAllow`
  allowlist (`tools/openapigen/main.go`) or it is silently omitted from
  schemas/examples (and `build-openapi.mjs` then fails on the missing schema).
- A new or renamed endpoint has a FOURTH step nothing checks: copy
  `frontend/public/openapi.json` → `docs/public/openapi.json`, then
  `cd docs && pnpm gen:api` to refresh the MDX under
  `docs/content/docs/en/reference/api/`. `docs-ci.yml` fires only on `docs/**`.
- A new English i18n key goes in EVERY locale JSON in `internal/web/translation/`
  (13 files) AND must be referenced from `frontend/src` or Go in the SAME commit —
  `frontend/src/test/i18n-dead-keys.test.ts` fails both ways. It is a frontend
  test, so run `npm test`, not just `make test-go`. At runtime the frontend falls
  back to en-US; Go (`internal/web/locale/`) returns "" for an unknown key.
- DB / model changes require a migration in `internal/database/db.go`.
- New persisted models must appear in both `allModels()` and the FK-ordered `migrationModels()` in `internal/database/migrate_data.go`; check their consistency before committing. A successful build does not verify this contract.
- Before pushing, run `make push-check` after committing. The Husky `pre-push` hook checks the actual pushed commit tips in isolated worktrees and blocks failures; fix the reported failure instead of disabling the hook. Local PostgreSQL/browser checks require the dedicated environments documented in CONTRIBUTING.md.
- Every state-changing inbound/client op dispatches through `runtime.Runtime`
  (`internal/web/runtime/`) — never straight to `internal/xray/api.go`, never from
  a controller or cron job. A direct call passes every local test and silently
  breaks every multi-node deployment. Other layering rules: `docs/architecture.md` §8.
- Conventional commits: `type(area): short imperative summary`, then a body
  explaining the why. Types in use: `fix`, `feat`, `chore`, `refactor`, `perf`,
  `docs`, `style`.

## Go conventions
- Stdlib `testing` only (no testify). Table-driven, `t.Run` subtests,
  `t.Helper()` on helpers. Assert the exact value / typed error / emitted
  string, never just `err != nil`. Prefer real deps over mocks: throwaway DB via
  `database.InitDB(filepath.Join(t.TempDir(), "x-ui.db"))` +
  `t.Cleanup(func() { _ = database.CloseDB() })`; `httptest` for HTTP.
  `internal/sub`'s `initSubDB(t)` is the template.
- A test must fail without its fix. Write it, revert the fix, watch it go red,
  restore. A test that passes either way is worse than no test: it certifies
  nothing and then gets cited as proof the fix works.
- Test what can actually break. No test for a getter, a constant, a rename, a
  pure map lookup, or inputs the function can never receive. One real test that
  drives the bug through the actual code path beats five that restate the code.
- Code must pass `golangci-lint run` (gofumpt + goimports formatting): `make lint`.
- Postgres, xray-gRPC-e2e and scale tests `t.Skip` unless `XUI_TEST_PG_DSN`,
  `XUI_DB_TYPE`+`XUI_DB_DSN`, `XRAY_E2E_BINARY` or `XUI_SCALE_TEST` is set — a
  green `go test ./...` does not mean those paths ran.

## Frontend conventions (summary; full version in frontend/CLAUDE.md)
- Ant Design 6 only — no Tailwind/shadcn. Targeted tweaks, not rewrites.
- TS strict; `@typescript-eslint/no-explicit-any` is an error. Zod schemas in
  `src/schemas/` are the source of truth; infer types with `z.infer`, never
  hand-write. Do not edit `src/generated/`.
- Node 26 (`.nvmrc`) — `make gen` imports `.ts` directly and needs its type
  stripping; Node 22 dies with `ERR_UNKNOWN_FILE_EXTENSION`. `npm test` includes
  a headless-Chromium Storybook project, so run
  `npx playwright install --with-deps chromium` once or `make verify` fails.
- Editing `frontend/src` does NOT change what users see until the Vite build is
  regenerated into `internal/web/dist/`. In `XUI_DEBUG=true`, HTML is served from
  the frozen embedded FS but JS/CSS off disk — after `npm run build` you MUST
  restart `go run .` or you get a blank page with 404s.
- After touching share-link logic (`src/lib/xray/`), run `npm run test` (golden
  fixtures); regenerate snapshots (`npx vitest run -u`) only for intentional
  output changes, never to make a red test green.

## Build, test, verify
A fresh clone has no `internal/web/dist/`, so a bare `go build ./...` dies with
`pattern all:dist: no matching files found` while ~35 other packages pass — it
reads as a broken repo, not a missing step. Run `make dist-stub` once; every
`make` Go target already depends on it, which is why `make test-go` beats
`go test ./...`. Run `make help` for all targets. The local gate:

    make verify   # gen-check + lint + typecheck + test + build + build-storybook

That is the *fast* gate, not all of CI. `ci.yml` also runs `make race`,
`make vulncheck`, a live-Postgres job (where a SKIP counts as a failure) and a
30s fuzz smoke on `FuzzParseLink`/`FuzzDecodeCertPin` — run those locally when
you touch DB/dialect or parser code.

Common targets: `make gen` (regenerate Zod/OpenAPI), `make lint` (Go + frontend),
`make test` (Go `-shuffle=on` + frontend), `make race`, `make build`. See `Makefile`.

## Definition of done (before opening a PR)
1. `make verify` passes — its `gen-check` already runs `make gen` and fails on a
   dirty `frontend/src/generated` / `frontend/public/openapi.json`.
2. Diff is focused; refactors are separate from feature work.
