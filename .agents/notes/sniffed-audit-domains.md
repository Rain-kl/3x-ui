# Audit domain attribution correction

Sniffing changed session routing targets but access messages retained their original To field. Record the observed domain separately rather than overwrite the original target, which administrators need for investigation. Extend the existing log/event boundary and reuse stored Host/Site columns, avoiding a new schema or sniffing subsystem. Capture before shouldOverride so routeOnly and override exclusions cannot suppress observed domains.

Maintenance: preserve optional log field escaping, both dispatcher paths, spool serialization and legacy fallback; verify actual HTTP traffic and unchanged original targets.

Real HTTP tests also exposed fallback parsing that stripped everything before the first colon, treating `//host:port` as a port-only website. Parse actual URL forms and only remove known tcp/udp prefixes; retain original destination strings. The ingestion regression uses a legacy HTTP URL alongside a sniffed IP target.
