# Sniffed domains in connection audit

Keep the original destination unchanged. When payload or FakeDNS sniffing successfully identifies a nonempty domain, export it as a separate percent-encoded `sniffed_domain` access-log field. Capture domains before destination-override decisions so `routeOnly`, exclusions and routing choices do not erase observed metadata. Failed/disabled sniffing emits no domain field. This does not change the actual connection target or routing rules.

The panel transports the optional domain through its existing bounded spool and node pull/ack path, uses it to populate existing Host/Site columns, and retains Destination as the original address. Legacy log lines and events remain readable. The audit UI shows domain and original destination together. No database migration is needed; previously recorded IP-only events are not retroactively recoverable.

Acceptance: IP-target HTTP requests produce their Host domain for both routeOnly settings; sniffing disabled preserves IP-only records. Node spool reload preserves the optional domain; website aggregation uses the observed domain while original IP and exact byte totals remain intact.

Legacy HTTP-proxy destinations use URL forms such as `//host:port` or `http://host/path`, while other protocols use `tcp:host:port`. Host fallback must parse both forms and never mistake the port for a website. Preserve the raw destination in all cases.
