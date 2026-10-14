#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root=$(git rev-parse --show-toplevel)
cache_root="$repo_root/.cache/push-checks"
task_tmp=$(mktemp -d "${TMPDIR:-/tmp}/xui-push-check.XXXXXX")
worktree=""
log_file=""
stage="initialization"
core_bin=""
postgres_test_dsn="${XUI_TEST_PG_DSN:-}"

cleanup() {
    local status=$?
    trap - EXIT
    if [ -n "$worktree" ]; then
        if ! git -C "$repo_root" worktree remove --force "$worktree"; then
            printf 'Cannot remove temporary worktree: %s\n' "$worktree" >&2
            status=1
        fi
    fi
    rm -rf -- "$task_tmp"
    if [ "$status" -ne 0 ]; then
        printf '\nPush blocked at: %s\nLog: %s\n' "$stage" "${log_file:-not created}" >&2
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

add_commit() {
    git -C "$repo_root" rev-parse --verify "$1^{commit}" >> "$task_tmp/commits"
}

case "${1:-}" in
    --hook)
        while read -r local_ref local_sha remote_ref remote_sha; do
            [[ "$local_sha" =~ ^0+$ ]] && continue
            [ "$local_sha" = "$remote_sha" ] && continue
            add_commit "$local_sha"
        done
        ;;
    --commit)
        [ "$#" -eq 2 ] || { printf 'Usage: %s --commit COMMIT\n' "$0" >&2; exit 1; }
        add_commit "$2"
        ;;
    '') add_commit HEAD ;;
    *) printf 'Unknown check option: %s\n' "$1" >&2; exit 1 ;;
esac

if [ ! -s "$task_tmp/commits" ]; then
    printf 'No commit updates to check.\n'
    exit 0
fi
sort -u "$task_tmp/commits" > "$task_tmp/unique-commits"
for tool in go node pnpm make golangci-lint curl unzip; do
    command -v "$tool" >/dev/null || { printf 'Required tool missing: %s\n' "$tool" >&2; exit 1; }
done
mkdir -p "$cache_root/logs"

run_stage() {
    stage=$1
    shift
    printf '\n[push-check] %s\n' "$stage" | tee -a "$log_file"
    (set -e; "$@") 2>&1 | tee -a "$log_file"
}

prepare_core() {
    local version base asset platform core_dir digest name
    version=$(./tools/latest-xray-feat-version.sh)
    base="https://github.com/Rain-kl/Xray-core/releases/download/$version"
    for asset in Xray-linux-64.zip Xray-linux-arm64-v8a.zip Xray-linux-arm32-v7a.zip \
        Xray-linux-arm32-v6.zip Xray-linux-32.zip Xray-linux-arm32-v5.zip \
        Xray-linux-s390x.zip Xray-windows-64.zip; do
        curl --fail --silent --show-error --location --head --connect-timeout 10 --max-time 30 --retry 2 "$base/$asset" >/dev/null
    done
    platform="$(uname -s)-$(uname -m)"
    case "$platform" in
        Linux-x86_64) asset=Xray-linux-64.zip ;;
        Linux-aarch64|Linux-arm64) asset=Xray-linux-arm64-v8a.zip ;;
        Darwin-x86_64) asset=Xray-macos-64.zip ;;
        Darwin-arm64) asset=Xray-macos-arm64-v8a.zip ;;
        *) printf 'Unsupported local core platform: %s\n' "$platform" >&2; return 1 ;;
    esac
    core_dir="$cache_root/core/$version/$platform"
    mkdir -p "$core_dir"
    if [ ! -f "$core_dir/$asset" ]; then
        curl --fail --show-error --location --connect-timeout 10 --max-time 300 --retry 3 "$base/$asset" -o "$core_dir/$asset.part"
        mv "$core_dir/$asset.part" "$core_dir/$asset"
    fi
    curl --fail --silent --show-error --location --connect-timeout 10 --max-time 60 --retry 3 "$base/$asset.dgst" -o "$core_dir/$asset.dgst"
    digest=$(awk '$1 == "SHA2-256=" {print $2}' "$core_dir/$asset.dgst")
    [[ "$digest" =~ ^[[:xdigit:]]{64}$ ]] || { printf 'Invalid SHA256 digest for %s\n' "$asset" >&2; return 1; }
    if command -v sha256sum >/dev/null; then name=sha256sum; else name='shasum'; fi
    if [ "$name" = sha256sum ]; then
        (cd "$core_dir" && printf '%s  %s\n' "$digest" "$asset" | sha256sum -c -)
    else
        (cd "$core_dir" && printf '%s  %s\n' "$digest" "$asset" | shasum -a 256 -c -)
    fi
    unzip -oq "$core_dir/$asset" -d "$core_dir"
    chmod +x "$core_dir/xray"
    core_bin="$core_dir/xray"
    [ "$("$core_bin" version | awk 'NR==1 {print "v" $2}')" = "$version" ] || { printf 'Core version mismatch\n' >&2; return 1; }
    printf '%s\n' "$core_bin" > "$task_tmp/core-bin"
    printf 'Verified published core: %s\n' "$version"
}

frontend_checks() {
    cd frontend
    HUSKY=0 pnpm install --frozen-lockfile --ignore-scripts
    pnpm run gen
    git diff --exit-code -- src/generated public/openapi.json pnpm-lock.yaml
    pnpm run typecheck
    pnpm run lint
    pnpm run format:check
    pnpm test --project unit --project components
    pnpm run build
    pnpm run build-storybook
    if [ "${PUSH_CHECK_BROWSER:-0}" = 1 ]; then
        pnpm test --project storybook
    fi
    cd ..
}

docs_generation_check() {
    cmp frontend/public/openapi.json docs/public/openapi.json
    cd docs
    HUSKY=0 pnpm install --frozen-lockfile --ignore-scripts --no-optional
    pnpm run gen:api
    git diff --exit-code -- content/docs/en/reference/api public/openapi.json pnpm-lock.yaml
    cd ..
}

model_contract() {
    go test ./internal/database -run '^TestMigrationModelsMatchPanelModels$' -count=1 -v | tee "$task_tmp/model-contract.log"
    grep -q '^--- PASS: TestMigrationModelsMatchPanelModels ' "$task_tmp/model-contract.log"
}

api_contracts() {
    go test ./internal/web -run 'Test(MasterNodeContract|RouteRegistryContract)' -count=1 -v | tee "$task_tmp/api-contracts.log"
    grep -q '^--- PASS: TestMasterNodeContract ' "$task_tmp/api-contracts.log"
    grep -q '^--- PASS: TestRouteRegistryContract ' "$task_tmp/api-contracts.log"
}

postgres_checks() {
    [ -n "${XUI_TEST_PG_DSN:-}" ] || { printf 'PUSH_CHECK_POSTGRES requires a dedicated XUI_TEST_PG_DSN\n' >&2; return 1; }
    env XUI_DB_TYPE=postgres XUI_DB_DSN="$XUI_TEST_PG_DSN" go test ./internal/web/service -run PostgresCommitFailure -count=1 -v | tee "$task_tmp/postgres-service.log"
    [ "$(grep -c '^--- PASS' "$task_tmp/postgres-service.log")" -ge 1 ]
    go test ./internal/database -run '^(TestHostAutoMigrateCreatesColumns_Postgres|TestMigrate_Postgres|TestClientWeeklyRenewMigration_Postgres|TestMigrate_LegacyBooleanVerifyPeerCertByNameIsBlanked_Postgres)$' -count=1 -v | tee "$task_tmp/postgres-database.log"
    [ "$(grep -c '^--- PASS' "$task_tmp/postgres-database.log")" -ge 4 ]
}

snapshot_clean() {
    git diff --exit-code
    local pending
    pending=$(git status --porcelain --untracked-files=all)
    [ -z "$pending" ] || { printf 'Checks generated uncommitted files:\n%s\n' "$pending" >&2; return 1; }
}

while read -r commit; do
    worktree="$task_tmp/tree-$commit"
    log_file="$cache_root/logs/$commit-$$.log"
    stage="create snapshot $commit"
    git -C "$repo_root" worktree add --quiet --detach "$worktree" "$commit"
    cd "$worktree"
    printf 'Checking commit %s in an isolated worktree.\n' "$commit" | tee "$log_file"
    unset XUI_DB_TYPE XUI_DB_DSN XUI_TEST_PG_DSN XRAY_E2E_BINARY XRAY_TRAFFICLOG_BIN
    if [ "${PUSH_CHECK_POSTGRES:-0}" = 1 ]; then export XUI_TEST_PG_DSN="$postgres_test_dsn"; fi
    export XUI_DB_FOLDER="$task_tmp/runtime-db" XUI_LOG_FOLDER="$task_tmp/runtime-logs" XUI_BIN_FOLDER="$task_tmp/runtime-bin"
    mkdir -p "$XUI_DB_FOLDER" "$XUI_LOG_FOLDER" "$XUI_BIN_FOLDER"
    run_stage 'tool and workflow configuration' node "$repo_root/scripts/check-ci-config.mjs"
    run_stage 'embed stub' make dist-stub
    run_stage 'model registration contract' model_contract
    run_stage 'API and node contracts' api_contracts
    run_stage 'Go static checks' golangci-lint run
    run_stage 'frontend generation, checks, tests and builds' frontend_checks
    run_stage 'documentation API generation' docs_generation_check
    run_stage 'fork removal invariants' node scripts/check-fork-removals.mjs
    run_stage 'published core assets, checksum and version' prepare_core
    core_bin=$(cat "$task_tmp/core-bin")
    [ -x "$core_bin" ] || { printf 'Verified core binary is missing\n' >&2; exit 1; }
    export XRAY_E2E_BINARY="$core_bin" XRAY_TRAFFICLOG_BIN="$core_bin"
    run_stage 'real core traffic logging and rate limits' go test github.com/xtls/xray-core/testing/trafficlog -count=1
    run_stage 'all Go package tests' go test -shuffle=on -count=1 ./...
    run_stage 'audit and node race checks' go test -race ./internal/audit ./internal/web/service -run 'Test(Audit|SetAudit|Node)' -count=1 -timeout 25m
    run_stage 'Go build' go build ./...
    if [ "${PUSH_CHECK_POSTGRES:-0}" = 1 ]; then run_stage 'PostgreSQL checks' postgres_checks; fi
    run_stage 'snapshot consistency' snapshot_clean
    printf '\nLocal checks passed for %s. Log: %s\n' "$commit" "$log_file"
    printf 'Remote CI also verifies PostgreSQL, full race/fuzz, dependency audits and browser jobs; optional local PostgreSQL/browser flags are documented.\n'
    cd "$repo_root"
    git worktree remove --force "$worktree"
    worktree=""
done < "$task_tmp/unique-commits"
