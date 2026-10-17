# mixed 统一客户端

mixed 使用独立账号表会绕开客户端关联、计费和撤销。新增客户端 username 字段区分外部登录名与入站隔离的内部统计 email，password 沿用现有字段。配置统一使用 clients；旧 accounts 仅在保存/导入时按入站生成稳定身份并转换到关联表。当前没有生产 mixed 数据，因此不在启动时自动改写历史账号。

HTTP/SOCKS 必须在核心内共享动态用户表；仅改面板无法可靠支持改密、计费和最后用户删除后的强制认证。核心依赖和运行二进制均需 v26.10.10-feat.4。面板在启动、热更新和子节点部署时检查版本。真实主子节点验证发现 client_inbound_apply 的底层 AddUser map 也必须传 username，否则改密后认证名会回退为 email；此处已补齐。

mixed 默认不进入 raw、JSON、Clash 自动订阅；统一查询只计入使用量。浏览器订阅页及 info 接口的 mixedNodes 单独查询当前订阅客户端的启用入站，批量读取用户名、密码、地址和配额，提供手动 HTTP/SOCKS5 复制。该区域不进入普通配置列表或复制全部操作；仅 mixed 的订阅仍可打开页面。禁用、过期及配额耗尽时隐藏凭据。

验收覆盖旧账号幂等迁移、客户端附加和用户名冲突、自动订阅隔离、IPv6/凭据编码，以及临时真实核心的 HTTP/CONNECT/SOCKS5、按入站客户端计费、改密、最后用户删除、来源前置拒绝。临时主子面板覆盖主控部署、改密、禁用、重新启用和解除关联。发布须先提供配套核心模块与 feat.4 二进制，再发布面板。


最终检查：Go 静态检查零问题；前端类型、lint、格式检查通过；前端 86 个单元测试文件共 1,400 项通过。mixed 手动页面、客户端凭据、远程路由组件测试通过；现有日历续期组件曾在并行运行时超过 5 秒，单独复跑通过。主子节点真实测试还覆盖到期及续期恢复。

全量回归的限制：Go shuffle 中未改动的 Discord `TestGatewayRequestedHeartbeatDoesNotRaceTicker` 在本地洪泛心跳场景发生 broken pipe/EOF；本次涉及的 service、sub、database、xray 及并发检查通过。核心全量测试另有公网 DoH/GitHub/Cloudflare 访问失败；定制核心的发布门禁和本次协议/认证测试通过。不得将这些结果表述为全量门禁通过。

热添加入站经 model.GenXrayInboundConfig 提交 protobuf，因此在该公共投影点将 username/password 转为 user/pass，并排除禁用账号；只改完整配置生成会遗漏这条路径。回归先证明未转换时热添加包含禁用账号，再验证实际核心 protobuf 的凭据和隔离计费身份。复制客户端同样生成新登录名，避免两个身份共用用户名。

最终前端完整回归为 198 个文件、1,890 项全部通过。完整 Go 门禁中暴露的 Discord 夹具 10ms 调度假设另以测试专用修改处理，见 [说明](ci-heartbeat-fixture.md)；不调整生产逻辑。
