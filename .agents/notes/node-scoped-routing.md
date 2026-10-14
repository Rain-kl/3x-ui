# 子节点远程路由即时生效

## 确认的原因与修复

子节点本地保存经 XraySettingController.updateSetting 调用 XrayService.RestartXray(false)，优先热更新；主节点则通过 Remote.PushRoutingRules 进入子节点 ServerController.setRouting。后者调用 ServerService.RestartXrayService，强制 RestartXray(true)，并将错误仅记录为警告后返回成功；当规则已写入模板时，再次保存因 changed=false 跳过运行态协调。因此主节点可能显示保存成功，但子节点实际配置未应用。

远程入口改为与本地一致的 RestartXray(false)。规则变更仍先验证并持久化，然后在核心运行时进行配置协调；相同的期望规则也会检查运行态，允许失败后重试。配置协调失败返回明确阶段和原始错误，Remote 的既有 envelope 错误机制将它传回主节点。停用核心不因保存规则被拉起。保留规则前缀还原、控制入站路由及原有热更新失败后的重启回退。

## 验证

server_routing_apply_test.go 使用实际 Rain-kl/Xray-core v26.10.10-feat.3，主节点 Remote 通过 HTTP 保存子节点路由后，立即用 RoutingService.TestRoute 验证出站从 direct 变为 blocked，并检查运行核心实例没有被替换。另将运行态恢复为旧规则，再提交与模板相同的规则，验证立即重新对齐；构造监听冲突验证首次保存和重复保存都向主节点返回失败，且旧核心继续运行。

命令：设置 XRAY_E2E_BINARY 为本地定制核心二进制，运行 go test ./internal/web/controller -run 'TestRemoteRoutingSave' -count=1。鉴权和入站前缀由既有节点契约和控制器回归覆盖。
