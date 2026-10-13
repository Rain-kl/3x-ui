# Audit retention architecture

Details are normally deduplicated by their existing IDs; history is grouped by UTC date/node/client/site. Delayed or unacknowledged archived visits use sparse ID/node/time receipts, including after ACK, until retention expiry. Do not keep source/target/request detail for old visits. Archive/delete and policy save/prune are atomic. Serialize query/ingestion/pruning/cleanup so summary cards and detail pagination share a consistent phase. Manual cleanup persists a monotonic timestamp watermark to prevent delayed spool delivery resurrecting deleted history. Daily ranges use calendar boundaries, short retained windows use exact hours. Keep measured counts separate from total connections.

Historical retention is exactly N UTC dates including today. Raw detail remains a rolling hour window, independent of midnight. Query returns Events/Total only in detailed hour mode. Container core downloads verify the release SHA256 too.
