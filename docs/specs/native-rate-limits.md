# Native Xray user rate limits

## Product behavior

The panel maps its existing Mbps downlink controls to native Xray user limits. The effective client cap for an inbound is selected in this order: client-inbound override, client-wide `DownLimit`, then the inbound's per-client default. The inbound-wide downlink cap is a separate shared group cap. The UI currently exposes downlink limits; the core config can carry both directions. Core speeds are bytes per second, converted from Mbps at 125,000 bytes/sec per Mbps.

Caps apply to authenticated users, aggregate concurrent sessions, and do not depend on IP address, NAT, or address family. Group identity is scoped to an inbound. Zero supplies no explicit cap at that level: client values inherit the next default, and transfers are unlimited when all effective caps are zero. Negative values and values whose Mbps-to-bytes/sec conversion overflows are invalid.

Supported user protocols are VLESS, VMess, Trojan, multi-user Shadowsocks, Hysteria, and MASQUE. WireGuard, TUIC, MTProto, AmneziaWG, and anonymous SOCKS/HTTP do not have a compatible authenticated-user limit path. Enabled unsupported inbounds with active positive client limits or positive inbound caps are rejected. Stored values can remain on a disabled inbound or inactive client so they can be re-enabled later; their stored caps are not applied while inactive.

## Core and lifecycle

Local native limits require Rain-kl/Xray-core `v26.10.10-feat.2` or newer. The panel blocks selecting an older local core when active local limits exist. Child-node inbounds carry configured limits through normal node synchronization; each child must run a compatible custom core because the control panel cannot enforce a child's local data path. Unsupported protocols are rejected instead of silently losing limits.

Changing effective limits restarts existing Xray sessions so the new bucket configuration applies to them. Rate values are persisted in the existing inbound/client fields, and ordinary client/inbound precedence remains the source of the effective per-user cap.

## Verification scenarios

Service tests cover generated native config and Mbps conversion (`TestGenXrayInboundConfigInjectsNativeRateLimits`, `TestGetXrayConfigAppliesNormalizedClientRateLimits`), restart behavior (`TestXrayServiceTryHotApplyRestartsForNativeRateChange`), version guards (`TestUpdateXrayRejectsUnsupportedNativeRateLimitVersions`), and unsupported protocol validation, including disabled stored settings and active clients (`TestNativeRateLimitValidationAllowsDisabledUnsupportedStoredCaps`).
