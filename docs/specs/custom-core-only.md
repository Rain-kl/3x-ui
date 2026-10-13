# Latest custom core only

The panel installs and upgrades with the latest published Rain-kl/Xray-core feat release. Remove the lock UI and ignore legacy lock settings/markers during panel installation and update. Remove official/custom arbitrary version selection. Existing API paths remain compatible, but installation accepts `latest` or the currently latest feat tag only; reject official/old tags before stopping the running core. Download only from the fork and keep checksum verification. No background unsolicited core restarts: panel upgrades replace the bundled core, and explicit core updates fetch latest.

Release validation must precede packaging/publication: core tests cover byte audit, observed domain logging, native rate aggregation and cancellation; panel tests run the actual selected release binary against audit/gRPC and native/core traffic-log fixtures. Test failures block publication. Legacy database lock keys are inert rather than requiring a destructive migration.

Acceptance: old lock markers cannot restore an old binary; official tag installation is rejected without network or process stop; API discovery and update UI offer latest feat only; broken native limit or sniffed domain behavior fails the release gate.
