# Bulk client downlink limits

## Product behavior

The existing client bulk-adjust action can set a global per-client downlink
limit for all selected clients, including clients selected through a group.
The input is an integer Mbps value. Leaving it blank means no change; setting
`0` clears the client-wide limit. Per-inbound client overrides remain intact
and continue to take precedence over the global limit, followed by the
inbound's per-client default.

Limits are applied using the same native Xray user-limit behavior as individual
client edits. A positive limit on an enabled inbound must use a supported
protocol and a compatible local Xray core. Unsupported active client/inbound
pairs reject the bulk request before any clients are changed. Disabled inbounds
and inactive clients may retain stored limits according to the native-limit
rules.

## Acceptance scenarios

- Selecting several clients and setting a positive Mbps value updates each
  client's global `DownLimit` and effective inbound settings.
- Setting `0` clears each global limit; existing per-inbound overrides and
  inbound defaults still apply.
- Leaving the field blank preserves all selected clients' global limits.
- The field help explains that clearing the global value does not remove per-inbound overrides or inbound defaults.
- The same control works when adjusting all clients in a group.
- Existing expiry, traffic, flow, HWID, and MTProto ad-tag adjustments remain
  available in the same request.
- An unsupported active protocol or incompatible running local core prevents
  the batch from making partial changes.
