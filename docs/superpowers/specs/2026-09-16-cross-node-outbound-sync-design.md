# Cross-Node Outbound Synchronization Design

## 1. Background & Goals
In 3x-ui multi-node architecture (Master <-> Worker Nodes), inbounds can be synced between nodes and master, but outbounds currently cannot. Operators wanting to configure transit/relay nodes must log into individual worker nodes to configure outbounds, rather than managing them centrally.

### Requirements:
1. **Centrally Added Outbounds Sync to All Nodes**: Outbounds created/updated/deleted on the master node automatically propagate to all online worker nodes.
2. **Worker Outbounds Converge to Master**: Outbounds configured on worker nodes are reported and adopted by the master node, and then propagated across the entire cluster.
3. **Deduplication by `protocol + address + port`**: Identify whether two outbounds refer to the same endpoint across nodes.
4. **Master Authority & Full Deletion Sync**:
   - Only proxy-type transit outbounds are synchronized (`vmess`, `vless`, `trojan`, `shadowsocks`, `hysteria`, `wireguard`, `socks`, `http`). Built-in outbounds (`freedom`, `blackhole`, `dns`, system tags) remain local.
   - Master configuration takes precedence in conflicts.
   - Deletions performed on the master propagate to all nodes, replacing worker proxy outbounds with the master's proxy outbound set while preserving local non-proxy outbounds.

---

## 2. Architecture & Data Flow

### 2.1 Outbound Identity & Key
A normalized key is computed for each proxy outbound:
`fmt.Sprintf("%s://%s:%d", strings.ToLower(protocol), strings.ToLower(host), port)`

Helper function: `ExtractOutboundKeys(outboundJSON map[string]any) []string`
Extracts all server endpoints defined in the outbound (`vnext`, `servers`, `peers`, `address/port`).

### 2.2 Worker Node Reporting (Pull & Adopt)
1. **Endpoint**: Remote node exposes its current template outbounds, or `FetchTrafficSnapshot` / endpoint `panel/api/server/outbounds` returns worker outbounds.
2. **Master Sync Loop**:
   - In `NodeTrafficSyncJob.syncOne`:
     - Worker's proxy outbounds are inspected.
     - Any outbound with an endpoint key not present in master's `xrayTemplateConfig` is adopted and appended to master's `xrayTemplateConfig`.
     - When master adopts new outbounds, it marks all other nodes dirty (`MarkOtherNodesDirtyTx`) and triggers an Xray reload if necessary.

### 2.3 Master Outbound Distribution (Push & Reconcile)
1. **Trigger**:
   - Master edits/deletes/adds outbounds via `XraySettingController.updateSetting` -> marks all enabled nodes dirty.
   - Master adopts new outbounds from any worker -> marks all other nodes dirty.
2. **Reconcile Process**:
   - In `InboundService.ReconcileNode` or `NodeTrafficSyncJob`:
     - When `n.ConfigDirty` is handled, call `ReconcileNodeOutbounds(ctx, rt, masterProxyOutbounds)`.
     - `rt` sends the master proxy outbounds to the worker.
     - The worker merges: keeps worker's local non-proxy outbounds (`freedom`, `blackhole`, `dns`, etc.), replaces all proxy outbounds with the master's proxy outbounds, saves worker's `xrayTemplateConfig`, and restarts/reloads worker's Xray.

---

## 3. Scope & Files Affected
- `internal/web/service/outbound_sync.go`: Deduplication, key extraction, proxy outbound merging & replacement logic.
- `internal/web/service/xray_setting.go`: Hook into `SaveXraySetting` / controller update to mark nodes dirty.
- `internal/web/controller/server.go`: Expose outbounds endpoint for remote RPCs (`panel/api/server/outbounds` GET & POST).
- `internal/web/runtime/remote.go`: Implement `FetchOutbounds` and `PushProxyOutbounds`.
- `internal/web/job/node_traffic_sync_job.go`: Connect the pull-and-adopt and push-reconcile steps.
- `internal/web/service/node.go`: Add `MarkAllNodesDirtyTx` or `MarkOtherNodesDirty`.
- `FEATURE.md`: Record feature description per repository guidelines.
