# Latest custom core only

The panel installs and upgrades with the latest published Rain-kl/Xray-core feat release. Remove the lock UI and ignore legacy lock settings/markers during panel installation and update. Remove official/custom arbitrary version selection. Existing API paths remain compatible, but installation accepts `latest` or the currently latest feat tag only; reject official/old tags before stopping the running core. Download only from the fork and keep checksum verification. No background unsolicited core restarts: panel upgrades replace the bundled core, and explicit core updates fetch latest.

Release validation must precede packaging/publication: core tests cover byte audit, observed domain logging, native rate aggregation and cancellation; panel tests run the actual selected release binary against audit/gRPC and native/core traffic-log fixtures. Test failures block publication. Legacy database lock keys are inert rather than requiring a destructive migration.

Acceptance: old lock markers cannot restore an old binary; official tag installation is rejected without network or process stop; API discovery and update UI offer latest feat only; broken native limit or sniffed domain behavior fails the release gate.

## 发布后重新集成

先发布并核验核心的全部必需平台 ZIP 与 SHA256，再构建面板。面板模块声明对齐已发布定制核心的上游基线，继续保留定制 fork 的精确提交替换。

Dev 更新检测以面板提交号为准；核心更新后的重新集成使用新的面板提交，使已安装上一提交的用户能够看到更新提示。发布完成后核对安装包校验和，并比较其中核心二进制与正式核心 Release 的内容摘要。
