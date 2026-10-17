# Discord 并发心跳测试的调度余量

mixed 完整门禁多次在未改动的 TestGatewayRequestedHeartbeatDoesNotRaceTicker 失败：夹具以 10ms 心跳周期同时洪泛服务端请求，CPU 争用时 ACK 尚未调度到读循环，生产侧的正常失联检测便先关闭 socket，测试将 broken pipe/EOF 误判为并发写损坏。

仅将该夹具发出的 hello 心跳周期改为 100ms。一秒洪泛仍跨越多个定时器 tick，同时保留服务端请求写路径、共享写锁以及“不能读到损坏帧”的断言；生产心跳、ACK 和超时处理不变。

修改前完整 Go shuffle 与独立包复跑均复现失败。修改后该测试连续执行 10 次通过，race 模式执行 3 次通过。前端完整回归 198 个文件、1,890 项通过；mixed 的真实主子节点、核心 API、数据库及订阅检查结果见 mixed-clients.md。核心全量测试的公网 DNS/GitHub/Cloudflare 限制另行记录，不将其算为通过。
