# ATM 开源边界决策

本文记录首次公开仓库采用的代码与权利边界。它不是法律意见；代码、素材和品牌的公开权利仍需权利人的书面确认。

## 已执行的边界

公开 ATM 的通用本地核心，包括：

- AI Agent 本地 transcript 解析、检索、统计和导出；
- Todo、Session Binding、Memory、Knowledge 和 Artifact；
- SQLite 存储、CLI、macOS 通用界面和 Agent hook；
- collection 存储、分析、审计、digest 与通用连接器注册表；
- 版本化的外部命令连接器与额度 provider 协议。

依赖公司身份、内网域名、内部 CLI、专有页面结构或专属品牌的实现不随公开核心发布。此前存在的服务专属消息适配器和浏览器额度扩展已从公开快照移除。私有或第三方集成可以独立发布，并通过 [`connector-protocol.md`](connector-protocol.md) 或 [`quota-provider-protocol.md`](quota-provider-protocol.md) 注册，无需链接进 ATM。

公开核心不捆绑凭据、登录态、真实消息或真实账号标识。连接器自行管理认证、服务权限和隐私政策。

## 历史策略

私有开发历史保留在原分支，不作为首次公开历史发布。公开分支从最终脱敏快照创建单一根提交，使用 GitHub noreply 作者邮箱，因此不会暴露旧提交中的公司邮箱、已移除代码或历史测试值。

这项策略不会改写或破坏私有分支，也不会改变其 tag；后续公开版本从公开根提交继续演进。

## 发布门槛

仓库可见性只能在以下条件全部满足后改为 Public：

- 权利归属和公司授权已有可追溯结论；
- 内部适配器边界已经执行；
- 公开分支是脱敏后的新根历史；
- secret/PII 扫描和人工复核无未接受风险；
- `./scripts/release-check.sh` 在最终公开快照中通过；
- GitHub 分支保护、私密漏洞报告和 secret scanning 已启用。
