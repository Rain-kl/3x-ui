# Local pre-push gate

Release validation exposed a missed persistent-model registration that compilation
and lint could not detect. The repository already has the necessary model and
route contract tests, but no pre-push enforcement. Wire the existing Husky hook
to a shared script and provide `make push-check` for manual execution.

Read Git's actual ref-update input, peel annotated tags to commits, deduplicate
tips and check each in a disposable detached worktree. This avoids testing HEAD
when another branch is pushed or allowing dirty local files to hide missing
committed generators/model entries. Frozen installs and generation run only in
the snapshot; final tracked and untracked status catches newly generated files.
Deletion-only pushes return without checks. Worktree cleanup is scoped to the
directories this invocation created. Logs/core cache live under ignored .cache.

Run configuration/model/route contracts first for fast failures. Then use the
existing lint/test/generator/build commands, published-core release resolution,
all-platform asset HEAD probes and per-platform checksum/version validation.
The real core binary is explicitly handed to integration tests. The stage runner
uses a strict subshell plus pipefail, so a failing intermediate function command
cannot be hidden by its last command; core-path transfer uses a temporary file
because pipeline functions execute in subshells. Requested optional PostgreSQL
checks assert actual PASS counts. Default limits are documented rather than
claiming equivalence with every remote CI job.

Full lint currently exposed ten pre-existing formatting/HTTP-constant violations
in audit and route-test code. Correct those narrowly so enabling the new default
gate does not leave the repository permanently unable to push. Developer tooling
changes do not add product FEATURES entries or FORK markers.
