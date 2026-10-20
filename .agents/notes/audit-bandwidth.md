# 审计实时带宽

## 数据来源

复用流量写入完成点，单次 JOIN 取得一个节点的所有 client_inbounds 原始累计字节，按客户端聚合差值/实际秒数。与按连接结束时间入库的审计流量分离，长连接可观察，不使用客户端计费倍率后的累计量。浏览器只读取内存快照，不触发节点 RPC。

## 边界与存储

每4秒最多采样一次；首次出现、计数回退、关联身份变化、采样间隔超过30秒均重建基线。15秒未更新即提示缺失；节点返回后的大额累计量不会伪装成实时峰值。实时横轴固定最近1小时，打开时以保存的分钟历史填充，然后拼接浏览器最多720个实时点；历史与实时重叠时优先实时，切换筛选重建。

每分钟按节点/客户端/进程 epoch 存储原始字节与独立覆盖时间，30秒批量幂等覆盖；重启后按 epoch 相加。存储与查询最多24小时，无历史回填。窗口不足一分钟的平均值按完整60秒分母计算；无覆盖画断点。节点级覆盖只证明节点采样连续，不证明所有客户端在整个分钟都存在；部分节点失联时汇总只包含已采集节点，界面同时提示缺失。

清理沿用审计锁和绝对水位，旧缓存不会刷回；完整导出包含分钟带宽表。崩溃最多丢失尚未刷盘的30秒数据。网站与访问日期无法准确归因到活动连接，明确不作用于此卡片。

## 验证

定向数据库测试覆盖速率计算、清零/中断、重复刷盘、客户端隔离、清理后不复活；迁移模型一致性、路由与生成模型契约由仓库推送护栏覆盖。前端类型检查、格式和构建随推送护栏执行。

## 2026-10-18 刷新前后精度一致

移除浏览器临时样本与分钟均值拼接。采集器为每节点保存最近一小时最多 901 次实际采样，只保留非零客户端速率并预计算节点合计；最近一小时接口按五秒桶返回覆盖/断点，按客户端筛选为哈希查找，全节点合计不逐客户端重复求和。浏览器刷新读取同一缓存，24 小时均值仍独立持久化；面板进程重启与清理后不伪造细粒度历史。

## 2026-10-20 bbolt rolling-hour samples

The current window already slides by now minus 3600 seconds; loss of the whole
preceding segment could not be attributed to a restart because uptime evidence
was unavailable. The confirmed durability gap was memory-only fine samples.
The user selected bbolt to close it. Store per-node sparse samples in an ordered
samples-v1 bucket alongside the SQLite file (or the configured PostgreSQL panel
data directory), outside the replaceable install directory, with 0600 mode.

Keep the sampling/query fast path in RAM, lazily restore once per database
instance, merge/deduplicate timestamps and retain at most 901 per node. Do not
restore raw counter baselines or replay samples into minute totals. A fresh
process therefore leaves a genuine sampling gap, not an artificial spike.
The existing 30-second/shutdown flush opens bbolt with a one-second lock timeout,
writes only new samples in one synced transaction and closes it. Only advance
save watermarks on success. Ordered prefix pruning visits expired keys; a new
cleanup watermark also removes crossing intervals. Free pages are reused,
physical file shrinking is not promised. Storage errors remain visible.

Clear commits the SQL cleanup watermark before synchronously removing bbolt and
RAM history; SQL watermark filtering prevents delayed writes/restoration from
resurrecting deleted samples. Restore happens before dashboard SQL snapshots,
so even a one-connection SQL pool cannot deadlock on a second pooled read.
Independent locks serialize store I/O, short memory locks protect snapshots;
ready sampling does not wait for disk I/O. Full audit export includes valid
recent samples as JSONL without exposing raw bbolt free pages. Coverage metadata
shows actual collected bounds; live chart bounds use server timestamps to avoid
browser clock skew. No new GORM model or SQL migration is needed.

Per standing user instruction, no checks/tests/build/hooks were run. Generation
and formatting are maintenance steps only; crash recovery and live deployment
behavior have not been runtime verified.

A bbolt open/write failure does not suppress the independent SQL minute flush:
finish both paths and aggregate errors, advancing each save watermark only for
its successful transaction. This prevents a cache durability fault from causing
unrelated 24-hour metric loss.

Pruning collects copied keys before deleting them, avoiding bbolt cursor index
shifts skipping adjacent expired records. PostgreSQL deployments still require
a persistent, writable panel data directory for this local bbolt file.
