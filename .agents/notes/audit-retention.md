# Audit retention architecture

Details are normally deduplicated by their existing IDs; history is grouped by UTC date/node/client/site. Delayed or unacknowledged archived visits use sparse ID/node/time receipts, including after ACK, until retention expiry. Do not keep source/target/request detail for old visits. Archive/delete and policy save/prune are atomic. Serialize ingestion/pruning/cleanup; read queries use a consistent database snapshot so summary cards and detail pagination share a phase without blocking ingestion. Manual cleanup persists a monotonic timestamp watermark to prevent delayed spool delivery resurrecting deleted history. Daily ranges use calendar boundaries, short retained windows use exact hours. Keep measured counts separate from total connections.

Historical retention is exactly N UTC dates including today. Raw detail remains a rolling hour window, independent of midnight. Query returns Events/Total only in detailed hour mode. Container core downloads verify the release SHA256 too.


## 主节点策略同步与清理广播

主节点持久化单调版本与绝对清理水位，事务提交后通过受信节点 RPC 广播，网络等待不持有审计锁。子节点策略入口独立于浏览器设置入口，并校验节点通道与直接主从拓扑。远程运行时缓存已成功的策略，变化立即下发，每分钟重新对齐；失败不缓存，审计任务在拉取前重试，重启后从持久化状态恢复。广播失败返回本机已保存及逐节点错误，不回滚已提交的主节点状态。整次广播等待不超过 15 秒，单节点仍限 8 秒，等待同一远程实例的同步槽也可取消。旧版子节点返回 404 时保留原有日志导出，限制告警频率；主节点水位仍阻止旧数据复活，子节点升级后自动对齐策略。

清理同时处理数据库和暂存队列；队列拒绝清理水位以前的再次捕获，绝对水位在断线重试中保持不变。子节点保留尚未确认的有效原始事件，不提前归档成丢失连接时刻的汇总。

## 性能检查与修复

- 原暂存文件在采集锁内逐条编码、写盘和 fsync，确认每批次都可能重写近 10 MiB 文件。改为入队时编码一次、单独串行化文件刷新，并在采集锁外写出不可变快照；版本计数保留写盘期间的新修改。
- 原迟到事件逐条检查明细和插入回执。改为最多 200 条的 SQL 批次，用 INSERT ... ON CONFLICT DO NOTHING RETURNING id 仅统计新回执，保留重试去重和原子汇总。
- 原仪表盘查询持有所有审计写入共用的锁，2 万条样本的多次聚合约需 63 ms。改用一致性读快照与两个读任务的并发上限：SQLite 显式 BEGIN DEFERRED 绕开写事务默认的 BEGIN IMMEDIATE，PostgreSQL 为只读可重复读。查询设置、明细与汇总来自同一快照，写入可并发继续；事务错误/超时回滚后释放连接。
- 本地基准（每轮 3 次迭代，优化后 3 轮中位数，非生产吞吐承诺）：1 万条、约 7 MiB 队列刷新约从 15.4 ms 降至 3.4 ms，分配从 10.9 MB 降至 1.85 MB；1000 条迟到事件入库约从 26.4 ms 降至 5.4 ms，分配从 9.57 MB 降至 2.18 MB。基准位于 spool_benchmark_test.go 和 audit_benchmark_test.go。
- 回归覆盖广播、部分失败、持久化重试、策略乱序、鉴权、完整清理、暂存队列持久化、磁盘刷新期间采集继续、仪表盘查询期间入库继续及响应快照一致性。汇总仍需扫描保留期内的数据，大规模部署应控制明细留存并关注数据库资源；本次没有用放宽去重或提前 ACK 换取性能。
