# Custom core policy and release gate

The operator has explicitly chosen latest custom cores for every installation and upgrade. Locking and arbitrary official core switching conflict with audit/rate feature compatibility, so remove them from user flows and update scripts. Keep existing endpoints for clients while validating the requested version against the latest fork release. Legacy lock state must not preserve old binaries.

Validate behavior before packaging/publishing, using the release binary actually bundled with the panel. This is a tested release gate, not a guarantee that all production-specific faults can be predicted. Keep all work on main and track fork integration markers.
