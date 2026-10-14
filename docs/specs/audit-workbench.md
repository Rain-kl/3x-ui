# Audit workbench consistency and control-plane exclusion

Use the shared page shell/card styles and light/dark/ultra theme classes used by client/inbound pages. Remove project sponsor placements from the dashboard, login page, desktop/mobile navigation, page routes, and public API. MTProto sponsored-channel ad tags remain a separate protocol feature.

The reserved `api` inbound is the panel control plane, not client browsing traffic. Consume its access lines without spooling, reject its legacy/node events at ingestion, exclude already-stored rows from every summary/detail query, and delete them during normal pruning. Do not exclude loopback targets from genuine client inbounds.

Acceptance: api events never change connection/byte/site counts, including old database rows; real client loopback traffic remains visible. No project sponsor UI route, placement, or public API remains. Audit shares other pages' padding, backgrounds and card styles on desktop/mobile and light/dark/ultra themes.
