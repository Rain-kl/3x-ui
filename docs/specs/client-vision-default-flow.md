# Default Vision flow for new clients

New clients created from the panel's add flows default the XTLS flow to
`xtls-rprx-vision` when at least one selected inbound is flow-capable
(`tlsFlowCapable`: VLESS on TCP with TLS/REALITY, or VLESS encryption on any
transport). The default is shown in the Flow field and can be changed to
`none`, which must persist for the rest of that add session.

The default applies only to new clients: editing an existing client keeps its
stored flow, including an empty one. The bulk add form follows the same rule.
When the selection contains both flow-capable and non-capable inbounds, the
form defaults to Vision and the existing per-inbound clamp still strips the
flow from the non-capable ones.

Programmatic client creation is unchanged: an omitted or empty `flow` on the
API remains empty. Only the panel forms choose the default.

## Acceptance scenarios

- Adding one client with a Reality/TLS VLESS inbound selected preselects
  `xtls-rprx-vision`; saving sends that flow.
- Choosing `none` after the default appears saves an empty flow.
- Adding a client with only non-flow-capable inbounds leaves the flow empty.
- Editing a client whose stored flow is empty does not fill in Vision.
- Bulk-add with a flow-capable inbound sends Vision for the created clients;
  with only non-capable inbounds it sends an empty flow.
