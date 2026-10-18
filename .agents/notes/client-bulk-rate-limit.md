# Bulk client rate-limit implementation

## Data flow

The batch-adjust modal accepts an optional integer `downLimit` in Mbps. An
omitted/null value leaves the global client limit unchanged; `0` clears it.
The value passes through the existing clients and groups callbacks, the
`bulkAdjust` request, controller, and `ClientService.BulkAdjust`.

The service validates selected active client/inbound pairs before any write,
then reuses the inbound-grouped settings update. For each inbound it derives
the applied value from the per-inbound override, requested global value, and
inbound default. It persists the global value to `ClientRecord`, flags local
Xray restart when the effective inbound config changes, and marks remote nodes
dirty through the existing transaction path.

## Maintenance

The UI uses the existing downlink-limit label/help translations. The help
translation was clarified in every locale because clearing the client-wide
value can still leave an inbound override or default active. The API endpoint
documentation includes the optional field and its semantics.

Verification for this change: frontend `pnpm typecheck`, frontend `pnpm lint`,
`go build ./internal/web/service ./internal/web/controller`, JSON parsing of
all locale files, and `git diff --check` passed. Tests were not run. `make lint`
could not complete because the installed golangci-lint cannot decode Go 1.27
export data (version 5; this linter supports up to version 4).
