# Configurable audit details and daily summaries

Defaults: retain individual visit times, sources, targets and exact bytes for 24 hours; retain daily traffic and request totals for 30 days. Settings allow 1..720 detail hours and 1..365 summary days, with summaries covering the detailed period. Increasing retention cannot restore already removed detail. Applying a shorter policy compacts and expires existing records in the same transaction as saving settings.

审计设置同时保存最近一分钟的连接/秒与网站/秒高频阈值，默认 10/1、0 关闭对应检测；阈值随版本化策略同步，旧客户端省略这些字段时保留已有值。分钟趋势计数固定保留 24 小时，独立于明细和每日汇总留存，清理操作同时清理分钟数据。速率口径与接口见 [审计工作台规格](audit-workbench.md)。

Archive by UTC date + node + client + site, with connection count, upload, download and measured count. Historical rows contain no individual requests or source/destination timestamps. Pruning runs every five minutes; queries enforce the detailed window immediately. Summary retention counts exactly N UTC calendar dates including today; detailed retention remains a rolling hour window. Fresh detail is never deleted merely because it spans an extra calendar date at the edge. Longer ranges cover calendar dates; a strict YYYY-MM-DD filter selects exactly one UTC date. Daily/history API responses contain only grouped data; they do not populate per-visit Events or Total. All metrics and pagination preserve node/client/site filters and exclude the api control plane. Queries use consistent database snapshots; ingestion, role transitions, cleanup and compaction share write serialization.

Late events inside the summary retention period enter daily aggregates directly. Sparse receipts retain only ID, node and time for delayed or unacknowledged archived visits, preventing retries from counting twice; receipts expire with history. Normally acknowledged visits need no historical per-visit IDs. Events older than retained history are acknowledged and discarded. Parent commits precede source ACK. Future clock skew up to five minutes is clamped; larger future timestamps are rejected.

The settings modal includes clear-all and clear-before-N-days actions, with explicit irreversible-action confirmations. The latter uses UTC midnight N dates ago, retaining the cutoff date. A monotonic cleanup watermark prevents unsynchronized older records from resurrecting cleared data. Cleanup never changes client quotas or disables new collection. Missing olderThanDays is rejected; zero explicitly means all. Server endpoints require administrator authentication.

Explicit migration adds AuditDailyStat and sparse AuditReceipt tables plus delivery status on details. Tests exercise totals, unknown-byte accounting, duplicate delivery, unacknowledged archive retries, calendar boundaries, filters, policy persistence/validation, cleanup watermark and child-role transitions.

## 主节点策略同步与清理广播

主节点是留存策略和清理水位的唯一所有者。保存设置或清理成功提交本机事务后，通过现有节点受信通道向所有启用的直接子节点广播完整策略：明细小时数、汇总天数、单调递增版本号和绝对 UTC 清理水位。广播并发最多 8，单节点超时 8 秒，整次广播最多等待 15 秒；网络等待不得持有审计锁。部分节点失败须明确返回“本机已生效、节点待重试”及节点错误，不能宣称全部成功。策略和水位持久化，周期审计同步在拉取前重新对齐；离线节点上线、新增节点、进程重启后均能恢复同步。

新增 POST /panel/api/audit/sync，仅允许经过认证的节点通道（node-sync/admin 令牌或 mTLS），普通浏览器会话不得调用。接收端必须是直接子节点（有启用的下游节点则拒绝）；接收策略时持久化子节点角色。重复请求幂等；旧版本不得覆盖新留存设置，清理水位永不倒退。主节点仍可独立调整策略，子节点的普通设置/清理入口继续拒绝本地操作。

清理使用主节点生成的绝对时间，避免节点时钟差异与离线重试误删新日志。清理覆盖明细、每日汇总、去重回执和内存/磁盘暂存队列；截止日期保留，清理全部时保留操作时刻之后的新连接。队列拒绝再次加入清理水位之前的事件。子节点的留存过期只删除过期数据，不把待导出的原始记录提前汇总，仍保持主节点提交后 ACK 的语义。增大留存不能恢复已经删除的数据。

验收覆盖：广播到启用节点、失败隔离和重试、断线恢复、重启后恢复、幂等与乱序、节点鉴权、子节点本地操作禁止、清理覆盖数据库及队列、旧记录不复活、清理期间的新日志保留。

## 性能约束

策略读取合并为单次设置查询；未变化的策略不在每个拉取批次重复落库或广播。暂存事件只编码一次，磁盘写入和 fsync 不持有日志采集锁，文件快照写入仍串行并保持原子替换。批量迟到事件按批次去重和写入回执，避免逐条 SQL 往返。性能检查以可重现的本地基准和查询次数为证据，不将测试环境数据等同生产吞吐量。

汇总查询使用数据库一致性快照，最多同时执行 2 个且设 30 秒期限，查询不得持有写入锁。SQLite 使用 WAL 下的延迟只读事务，避免默认 BEGIN IMMEDIATE 抢占写锁；PostgreSQL 使用只读可重复读事务。同一次响应内的留存策略、明细和汇总来自同一快照，写入可同时继续。回归验证查询期间入库不阻塞且响应不混用新旧计数。

旧版子节点缺少 audit/sync（HTTP 404）时，广播明确报告未同步，周期任务限制告警频率并保留旧版日志导出。主节点水位仍过滤已清理数据；子节点需升级后才能应用策略和主动清理暂存，其他同步错误则暂停该轮拉取，等待重试。
