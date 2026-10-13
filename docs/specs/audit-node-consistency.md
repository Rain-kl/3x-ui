# Node audit consistency

Authenticated requests to the node pending-audit export persist child mode, including when the existing connection uses an admin token. A standalone panel is not marked child merely because it has admin credentials. Serialize role transition, local ingestion and source acknowledgement so the child cannot drain its queue into a separate local database during export.

Pending export merges legacy locally stored retained events with the bounded spool, preserving IDs, original destinations, observed hosts, byte counts and chronological order. Parent commit precedes source ACK; ACK removes the matching legacy detail rows and spool events. IDs deduplicate retries centrally. Still-retained legacy visits and daily buckets are recovered. Buckets carry their original connection/byte/measured totals with a stable export ID; they enter master daily summaries without recreating visit details. Source ACK removes exported buckets. Already expired/deleted data cannot be reconstructed.

Child audit UI explicitly directs users to the master rather than showing a separate, misleading total. Master filters apply to imported node ID and all records. Keep api exclusion and fresh-event retention. Test admin-token role transition, queue/DB backfill, retries, ACK removal, no standalone misclassification and new local events after transition.

Support direct master/child topology. Export rejects panels with enabled downstream nodes before persisting a child role; export/ACK touch only local node_id=0 legacy visits. This prevents nested-master pulls relabeling/deleting downstream node history.

New masters request pending?summaries=1 to advertise support for bucket migration. Without that opt-in, children leave daily buckets unacknowledged locally rather than sending counts to an older master that would interpret each bucket as one visit. Upgrade the master before its children. Child retention/cleanup APIs reject changes; manage them centrally on the master.
