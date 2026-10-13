# Native Xray rate-limit integration

The panel keeps its existing limit fields and translates effective Mbps values into Xray's bytes-per-second fields. Per-client selection is inbound override, client default, then inbound per-client default; inbound aggregate downlink is a separate inbound-scoped group. Zero supplies no explicit cap and follows the next default; all effective caps at zero are unlimited. Invalid negative or overflowing values are rejected.

The core feature requires `v26.10.10-feat.2`. Local version selection is guarded when active limits exist. Node-bound settings travel with node synchronization, so the child must independently use a compatible core. Limit changes restart existing sessions. Unsupported active protocol limits are rejected, while disabled inbound and inactive-client values may be retained for later reactivation.

TC traffic shaping was removed. It depended on root privileges, qdisc/device state, and IP polling, which could not provide authenticated-user semantics across NAT and address changes. The native core token buckets account application bytes per authenticated user and inbound-scoped group. See [the specification](../../docs/specs/native-rate-limits.md) and [implementation notes](../../FEATURE.md).

## Fork synchronization and verification

Merged remote main through `4958fa19` to preserve the concurrently added maintenance guidelines. Product integration sites are marked; `node scripts/check-fork-removals.mjs` guards the removed TC module/hooks and new core wiring. JSON translations cannot contain comments; their nativeRateLimitHint keys identify the UI guidance. The Go dependency is pinned to the same fork commit as `v26.10.10-feat.2`; future release discovery still selects the latest stable feat runtime binary.

The repository has no architecture:check or fork:check-removals package scripts. The latter is invoked directly through its new script; Go package, lint and real core gRPC checks verify the runtime boundaries. Local pnpm Corepack activation failed in the proxy environment, so equivalent existing npm scripts ran typecheck, lint, unit tests and production build without changing the lockfile.
