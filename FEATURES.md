# Product features

## Native core user rate limits (native-rate-limits)

- **Background:** TC/qdisc shaping and client-IP polling cannot reliably cap authenticated clients across NAT, address changes, and concurrent sessions. The panel needs limits tied to Xray user identities.
- **Change:** Maps existing Mbps downlink settings to native bytes-per-second user caps, with inbound override > client-wide value > inbound default and a separate inbound-shared downlink group. Active unsupported protocols and incompatible local core versions are rejected; inactive stored caps remain available. Changes restart existing sessions. TC shaping is removed.
- **Files:** Panel integration spans `internal/web/service/`, `internal/xray/`, runtime/API wiring, and inbound/client configuration UI. See [FEATURE.md](FEATURE.md) for implementation detail and [the spec](docs/specs/native-rate-limits.md) for behavior and bounds.
- **Upstream fork markers:** Product edits to upstream files use `FORK(native-rate-limits)` markers. TC removal is recorded in the fork-removal guard.
- **Core provenance:** Rain-kl/Xray-core native implementation is based on upstream [PR #6050](https://github.com/XTLS/Xray-core/pull/6050), original commit `da56e92`, cherry-picked as `4e99a70`; see the [core feature entry](https://github.com/Rain-kl/Xray-core/blob/main/FEATURES.md#native-authenticated-user-rate-limits-native-rate-limits) and [core specification](https://github.com/Rain-kl/Xray-core/blob/main/docs/specs/native-rate-limits.md).

- **移除回归守卫**：`node scripts/check-fork-removals.mjs` 验证 TC 模块与旧钩子缺席，并检查原生配置、API 与重启接线。

## Sniffed domains in audit (sniffed-audit-domains)

- Preserve original IP destinations and export observed HTTP/TLS/QUIC/FakeDNS domains separately for website audit, including route-only sniffing.
- Core integration: common/log/access.go and both app/dispatcher dispatch paths; panel: internal/audit/spool.go, audit ingestion and audit event display.
- Design: [specification](docs/specs/sniffed-audit-domains.md). Implementation decision: [Agent Note](.agents/notes/sniffed-audit-domains.md). Upstream markers: FORK(sniffed-audit-domains). Legacy logs remain compatible; no retroactive IP-to-domain inference.

## Latest custom core only (custom-core-only)
- Always bundle/use the latest Rain-kl/Xray-core feat release; remove locking and official/arbitrary core switching. Legacy lock state no longer preserves old binaries during upgrades.
- Validate the selected binary's audit and user rate-limit behavior before publishing panel packages.
- Design: [spec](docs/specs/custom-core-only.md). Implementation and sync notes: [Agent Note](.agents/notes/custom-core-only.md). Integration marker: FORK(custom-core-only).

## Audit workbench consistency (audit-workbench)
- Share the existing page shell, cards and theme variants; remove desktop/mobile sidebar sponsorship.
- Exclude reserved api-inbound control traffic before spooling and from ingestion/queries; prune historical control records without hiding client loopback traffic.
- [Spec](docs/specs/audit-workbench.md), [Agent Note](.agents/notes/audit-workbench.md). Marker: FORK(audit-workbench).

## Audit retention (audit-retention)
- Preserve 24h visit details and 30 UTC calendar days of compact node/client/site totals. Historical summaries expose no per-visit timestamps; fresh detail and summary metrics share filters.
- Configurable retention and confirmed cleanup; atomic compaction, explicit migration, UTC date filtering, and sparse deduplication for delayed delivery.
- [Spec](docs/specs/audit-retention.md), [Agent Note](.agents/notes/audit-retention.md). Marker: FORK(audit-retention).

## Consistent node audit export (audit-node-consistency)
- Persist child role from authenticated export, recover existing fresh local records and stop child queue consumption; ACK after parent commit keeps stable ID deduplication.
- Child UI directs administrators to the central master. [Spec](docs/specs/audit-node-consistency.md), [Agent Note](.agents/notes/audit-node-consistency.md). Marker: FORK(audit-node-consistency).
