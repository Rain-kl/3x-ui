# Release CI performance

Run 39207307047 took 12m42s. validate-core took 8m07s, then seven builds
ran for up to 3m50s. Frontend was built eight times. Cross builds restored
one 670MiB native test cache and never saved target compilation results
because setup-go reported an exact immutable-key hit. The race command
also selected no tests in internal/audit.

Split frontend into a single verified artifact producer. Split native validation
into integration/race matrix jobs and overlap platform builds with validation.
Move tagged release writes out of build jobs into a validation-dependent final
publisher; dev publishing also waits for validation. Filter downloaded release
artifacts so the new frontend artifact cannot leak into published packages.
Retain all existing validation and run the entire audit package under race.

Separate immutable module caches from per-run compiler snapshots. Compiler keys
include OS, host architecture, target/check kind, go.mod and toolchain identity;
restore from the previous matching dependency snapshot, then the matching
Go/toolchain prefix. Go itself validates individual cached compilation inputs.
Pin all seven Bootlin toolchains to 2026.08-1, confirmed in the source run logs,
and cache their extracted trees. Parallelize gzip using runner-provided pigz;
disable redundant artifact compression for already compressed release archives.

User explicitly requested skipping local validation for this push. No local
tests, build, push-check or hook validation were run; HUSKY=0 applies only to
the requested commit/push invocation. Remote release validation remains enabled.
Cold-cache and warm-cache timing still require observation in GitHub Actions.
Spec: docs/specs/release-ci-performance.md.
