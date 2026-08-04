# ATM 发布就绪状态

本文记录 ATM 当前功能完备度、各 Agent 支持矩阵，以及正式提供给他人使用前还需要完成的事项。

## 结论

ATM 当前适合给熟人、团队内部或早期用户试用。核心功能已经闭环：

- 会话同步、搜索、展示、导出
- 实时状态与数据源诊断
- token/cost 统计，支持逐请求明细
- SQLite 原子 todo/binding 管理、自动关联 Agent session
- JSON 输出适合脚本消费，空列表稳定输出 `[]`
- macOS / Linux 的主要本地能力已覆盖，剪贴板和通知有常见命令 fallback

如果要公开给陌生用户使用，建议先完成“发布前必做”清单。

## 开源前门禁

在把仓库可见性改为 Public 之前，必须完成：

- [ ] 确认代码、设计、品牌和素材的权利归属，取得适用的公司开源授权。
- [x] 从公开核心移除服务专属消息适配器和内网页面扩展，保留通用命令连接器协议。
- [x] 用合成数据替换公开快照中的身份、消息标识、公司域名和个人路径。
- [x] 使用单一根提交建立公开分支，不携带私有历史和公司作者邮箱。
- [x] 提供项目许可证、第三方素材归属、安全报告流程、隐私说明和贡献指南。
- [x] 让 `./scripts/release-check.sh` 在最终公开快照中完整通过；公开后由 CI 再验证一次。
- [x] 提交最小权限 CI、CodeQL、依赖更新和仓库协作模板。
- [x] 复核二进制素材来源，并随包提供适用的第三方声明和许可证副本。
- [ ] 在 GitHub 启用分支保护、私密漏洞报告和 secret scanning。

其中权利归属、内部能力边界和历史隐私复核是发布阻断项，不能用测试通过替代。
具体拆分建议和历史处理选项见 [open-source-boundary.md](open-source-boundary.md)。

## 支持矩阵

| 能力 | Claude Code | Codex | Pi | Copilot | Qoder | Qoder CLI | QoderWork | Grok Build |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| 会话发现与同步 | yes | yes | yes | yes | yes | yes | yes | yes |
| 实时状态 | yes | yes | yes | yes | basic | yes | yes | yes |
| 全文搜索 | yes | yes | yes | yes | yes | yes | yes | yes |
| 完整 Q/A 展示 | yes | yes | yes | yes | basic | yes | yes | yes |
| thinking 展示 | yes | no | yes | no | no | no | no | no |
| 工具调用统计 | yes | yes | yes | yes | basic | yes | yes | yes |
| token/cost 汇总 | yes | yes | yes | no | yes | basic | basic | yes |
| 逐请求 usage_events | yes | yes | yes | no | no | basic | basic | yes |
| quota | no | yes | no | no | no | no | no | yes |
| 刘海 hook 推送 | yes | yes | yes (扩展) | no | no | no | no | yes |

说明：

- `basic` 表示能提供基础状态或基础内容，但依赖上游数据结构，字段完整度不如主力 Agent。
- Copilot 当前主要用于会话检索和工具统计，暂不提供 token/cost。
- Qoder 与 QoderWork 从本地 SQLite 读取；Qoder CLI 读取 JSONL transcript。上游 schema 变化时需要同步更新 parser。

## 发布前必做

1. 确认 GitHub Release 可用。
   - 打 `v*` tag 后检查 GoReleaser 是否生成 darwin/linux/windows 资产。
   - 验证 `install.sh` 能从 Release 下载对应平台包。

2. 清理仓库交付面。
   - 构建产物只放入已忽略的 `bin/` 或 Release，不放在源码根目录。
   - 保持真实 transcript、数据库、导出、配置、凭据和个人路径不进入 Git。
   - 运行当前文件与完整 Git 历史的 secret/PII 扫描，并人工复核高熵标识和内部域名。

3. 做 clean machine 验证。
   - `go install github.com/zane-byte-dev/atm/cmd/atm@latest`
   - `curl -fsSL https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh`
   - macOS: `atm session clip`、todo 完成通知
   - Linux: `wl-copy`/`xclip`/`xsel` 至少一个可用时的 `atm session clip`，以及 `notify-send`

4. 补一轮真实样本回归。
   - Claude continuation session
   - Codex 模型切换和 quota
   - Grok Build session usage + billing log quota（`~/.grok/logs/unified.jsonl`）
   - Pi thinking、增量同步、task run 关联
   - Copilot transcript
   - Qoder local.db、Qoder CLI JSONL、QoderWork agents.db

## 发布后可继续增强

- 为 `atm doctor` 增加修复建议，例如缺少剪贴板命令、数据源路径不存在、未发现 Release 资产。
- 为 `atm config init` 增加交互式向导。
- 为 Copilot 和 Qoder 补齐更多 usage/cost 明细，前提是上游数据源提供可靠字段。
- 增加 shell completion 安装说明。
- 增加更多端到端测试，覆盖真实 CLI 输出和迁移升级路径。

## 验证命令

发布前至少运行：

```bash
./scripts/release-check.sh
HOME=/tmp/atm-smoke /tmp/atm-check config init
HOME=/tmp/atm-smoke /tmp/atm-check doctor --json
HOME=/tmp/atm-smoke /tmp/atm-check session list --json
HOME=/tmp/atm-smoke /tmp/atm-check stats --json
```

`release-check.sh` 会运行 Go 测试与 vet、带版本注入的 CLI 构建、GoReleaser 全目标交叉编译、
安装器资产命名检查；在 macOS 上还会让 Swift 测试直接调用刚构建的真实 CLI，验证
versioned `dashboard` 聚合协议的 JSON 解码、代表性 v4 Todo/Binding/Comment 到 SQLite schema v12
迁移、旧文件备份保留，以及 CLI 错误在 App 中保留原始原因。期望空数据命令输出有效 JSON，列表字段
为空时为 `[]`。
