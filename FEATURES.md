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
