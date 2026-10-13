# Repair child standalone ingestion

The source only recognized node-sync tokens or an environment flag. Children connected via accepted admin tokens ingested and acknowledged their own spool every five seconds. Master pulls saw only leftovers, causing differing connection/byte totals. Persist role on the existing authenticated export endpoint, recover legacy fresh records, and atomically stop local draining. Do not identify every admin token as a parent relationship.

First role transition recovers retained raw visits and node-local daily summaries. Summary batches require summaries=1 capability opt-in and preserve original connection/measured totals. Export IDs are random, persisted once, and removed only after ACK; different children must never derive the same ID from shared date/client/site keys. Summary export index is partial, excluding normal unexported master buckets.
