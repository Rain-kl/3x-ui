# Bandwidth integration test timing

Run 39213093040 failed only validate-core (race):
TestAuditBandwidthCapturesRawCounters returned 978.71/1957.42 B/s, outside
its hardcoded 990..1010 / 1980..2020 ranges. The test set the prior sample
to wall-clock minus five seconds before doing a database update. Update and
scheduler overhead extend the actual interval; race instrumentation made that
overhead exceed the test's one-percent allowance.

Production correctly divides raw byte deltas by the actual sample interval.
Use the captured sample timestamp and baseline timestamp to verify the
5000/10000-byte deltas instead of assuming exactly five seconds. Retain the
10x billing ratio, freshness and node-isolation assertions; deterministic
observe tests already assert exact 1000/2000 B/s for fixed five-second inputs.
No production behavior or workflow gate is relaxed.

Spec: docs/specs/audit-bandwidth.md (CI sampling assertions).

Validation: go test -race ./internal/web/service -run
'^TestAuditBandwidthCapturesRawCounters$' -count=3 passed (3.617s test time).
Full push validation is omitted per the user's ongoing CI optimization/push
instruction; release validation on GitHub remains enabled.
