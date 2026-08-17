# ATM — AI Team Manager

给人用的 AI 管理面板。看 AI 都在干什么、干得怎么样、花了多少钱。

本地优先、单机单用户：它读各家 AI CLI 已经写在你硬盘上的会话记录，索引成一个能查、能统计、
能挂待办的库，再配一个 macOS 菜单栏 App。它不代理你和 AI 的对话，停掉它也不影响任何会话。

ATM 会索引可能包含源码、提示词、模型回复和个人信息的本地数据。公开使用或参与开发前请读
[隐私与数据处理](PRIVACY.md)、[安全报告流程](SECURITY.md)和[贡献指南](CONTRIBUTING.md)；
提交 issue、日志或导出前请先脱敏。

## 安装

**一键安装（推荐，无需 Go）**：

```bash
curl -fsSL https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh
```

**go install**：

```bash
go install github.com/zane-byte-dev/atm/cmd/atm@latest
```

**从源码构建**（需要 Go 1.25+）：

```bash
make install    # 构建并安装到 /usr/local/bin
```

## 上手

装完不用配置，直接跑——数据源路径会自动检测：

```bash
atm doctor              # 认出了哪些 AI 工具、明细覆盖率如何
atm session status      # 现在各个 AI 在干什么
atm now                 # 我这边工作中、等待中、待验收的事
atm stats --days 7      # 这周花了多少 token 和钱
```

## 命令

完整命令与参数一律看 `atm <命令> --help`（比如 `atm todo run --help`）——那里是唯一不会过期的
真相。下面只列日常最常用的：

**看 AI 在干什么**（别名 `atm s`）

```bash
atm session status                      # 实时状态
atm session list --days 7               # 最近会话
atm session search <关键词>              # 全文搜索历史
atm session show <session-id>           # 完整 Q/A，--thinking 带思考过程
atm session clip <关键词>                # 复制 AI 回复到剪贴板
```

**待办**

```bash
atm todo add "<标题>"                    # 加一条；--refine 立刻润色并按需拆分
atm todo list                           # 看列表
atm todo start <id>                     # 进入工作中
atm todo prompt <id> --copy             # 复制一行提示，粘贴进新的 Agent 会话
atm todo run <id>                       # 后台派发给 Codex（默认沙箱受限）
atm todo tail <id> -f                   # 跟随派发日志
atm todo submit <id> --reason "实现及证据" # 提交待确认
atm todo done <id>                      # 验收完成
atm todo trash <id>                     # 移到回收站（可恢复）
```

**统计与诊断**

```bash
atm stats --by model|skill|day|speed    # 换维度看用量
atm report [date]                       # 每日活动报告
atm sync                                # 手动同步（查询命令默认只读）
atm diagnose --bundle                   # 报障用支持包，脱敏且不联网
atm config init                         # 初始化配置文件
atm backup                              # 归档无处重建的记录（todo/记忆/知识/收集账本）
atm restore <archive>                   # 从归档恢复
```

**AI Day**

```bash
atm day today                           # 生成今天的概念、徽章和证据
atm day atlas                           # 查看 12 枚徽章与等级进度
atm day history --days 90               # 查看历史日历
atm day feedback 2026-08-15 --verdict corrected --badge code_architect
atm day privacy show                    # 检查语义分类、保留期与来源权限
atm day sources pause codex             # 暂停一个来源
atm day export --json > ai-day.json     # 导出全部衍生数据（不含原文）
```

AI Day 在本机把会话镜像归一化为不含原文的事件，按最近 30 个有效日生成每日唯一结果。完整的数据模型、
12 枚徽章、评分规则、隐私与删除命令见 [AI Day 说明](docs/ai-day.md)。

**知识与记忆**

```bash
atm knowledge search <query>            # 跨库搜索中央知识
atm knowledge add <标题> --file note.md  # 写入
atm memory recall [query]               # 召回共享记忆
atm memory remember <内容>               # 记一条
```

**外部收集**（连接器，默认关闭）

```bash
atm collect status                       # 健康状态、来源、处理记录
atm collect source add --connector slack --kind channel --id C123 --project atm
atm collect enable                       # 开启 App 常驻期间的后台自动收集
atm collect run                          # 立即增量收集一次
```

全局 flag：

- `--agent` — 按 agent 过滤：`claude`、`codex`、`pi`、`copilot`、`qoder`、`qodercli`、`qoderwork`、`grokbuild`
- `--json` — JSON 输出（list、search、status、show、stats 等均支持）
- `--sync` — 查询前显式同步会话源；查询默认只读，不会修改数据库

## macOS 菜单栏 App

独立菜单栏 App 常驻显示今日 Token 和「需要你 N」，主窗口提供任务、收集、Agent、知识、用量和 AI Day 六个
工作区。会话详情的「对话」支持摘要、时序、完整三种读法；收集工作区把钉钉等外部来源的消息分类成
Todo 或知识沉淀，并可重试、纠错与撤销。

```bash
app/macos/Scripts/build-app.sh
open app/macos/dist/ATM.app
```

开发与路径配置见 [`app/macos/README.md`](app/macos/README.md)。

## 通知

Agent 卡住等你时，ATM 发一条系统通知，点击直接跳到它所在的终端；Agent 继续往下走之后通知自动撤回。
菜单栏同时显示「需要你 N」，不看通知也能一眼扫到。

**通知只在装了 hook 之后才有**——不装的话 ATM 只能猜，而 Agent 卡在工具授权那一刻根本不会写下任何
文字（[为什么](docs/internals.md#通知与-hook)）。

```bash
atm agent hook install            # Claude / Codex / Grok Build / Qoder 都装
atm agent hook status             # 看当前接了哪些事件
atm agent hook uninstall          # 原样摘掉
```

装的都是只上报的 hook，不拦工具调用、不替你做授权决定，且只增删 ATM 自己那几条配置。
Qoder 装完要重启才生效；Pi 需要手动复制
[`integrations/atm-notch.ts`](integrations/atm-notch.ts) 到 `~/.pi/agent/extensions/`。
也可以在 App 的「设置 → 通知」里一键安装。

## Agent 集成

把 ATM 作为 prompt/skill 挂到你的 AI agent，让 agent 直接帮你查状态、管待办：

- **pi**：把 [`integrations/pi-prompt.md`](integrations/pi-prompt.md) 复制到
  `~/.pi/agent/prompts/atm.md`；可将 [`integrations/pi-atm-attention.ts`](integrations/pi-atm-attention.ts)
  安装到 `~/.pi/agent/extensions/`，在每个会话首次执行前只注入当前绑定或最多 3 条仓库候选
- **Codex**：SessionStart hook 可调用 [`integrations/codex-atm-context.sh`](integrations/codex-atm-context.sh)，
  避免把完整 `atm now --json` 反复塞入上下文
- **其他 agent**（Claude Code 等）：以该文件内容作为 prompt/skill 参考

## 数据源

| Agent | 路径 |
|---|---|
| Claude Code | `~/.claude/projects/**/*.jsonl` |
| OpenAI Codex | `~/.codex/sessions/YYYY/MM/DD/*.jsonl` |
| Pi | `~/.pi/agent/sessions/--<path>--/*.jsonl` |
| GitHub Copilot | `~/Library/Application Support/Code/User/workspaceStorage/*/GitHub.copilot-chat/transcripts/*.jsonl` |
| Qoder | `~/Library/Application Support/Qoder/.../local.db` (SQLite) |
| Qoder CLI | `~/.qoder/projects/**/*.jsonl` |
| QoderWork | `~/Library/Application Support/QoderWork/data/agents.db` (SQLite) |

所有路径可通过 `~/.atm/config.json` 自定义，支持 macOS / Linux 自动检测。

各家 Agent 都会轮转自己的日志（Claude Code 默认清理一个月前的 `~/.claude/projects`），但
**源文件消失不等于会话被删除**：ATM 保留整条会话，`atm stats --days 90` 的历史不会自己缩水。
索引不做自动清理，删除只有 `atm session forget <id>` 这一条显式路径。详见
[保留策略](docs/internals.md#保留策略)。

## 配置

配置在 `~/.atm/config.json`（`atm config init` 生成）。API Key 单独存 `~/.atm/credentials.json`，
用 `atm config credential set` 从 stdin 写入，不进普通配置、备份、诊断包或日志。

费用按模型定价计算，可在 `pricing` 字段覆盖或新增模型，每项为
`[input, output, cache_create, cache_read]`（美元/百万 token）：

```json
{
  "pricing": {
    "claude-opus-4-6": [15.0, 75.0, 18.75, 1.50]
  }
}
```

未配置的模型回退到内置默认价。

## 输出约定

- stdout 给机器读（JSON），stderr 给人读（sync 进度）
- `--json` 模式下 stdout 是纯净 JSON，可直接 pipe 到 `jq`
- 空列表统一输出 `[]`，不会输出 `null`
- 时间戳使用 ISO 8601 格式（`2026-06-24T10:05:25+08:00`）

## 平台能力

- macOS / Linux 支持默认数据源路径自动检测；所有数据源路径可通过 `~/.atm/config.json` 覆盖
- `atm session clip` 支持 macOS `pbcopy`、Linux `wl-copy`/`xclip`/`xsel`、Windows `clip`
- todo 人向通知支持 macOS `terminal-notifier`/`osascript` 和 Linux `notify-send`：新建、待验收
  （submit/review）、完成、放弃都会提醒；`--json` 同样发送；缺少通知命令时静默跳过，不影响任务状态。
  菜单栏 App 在刷新时也会对外部新建/进入待验收发原生通知。设 `ATM_SKIP_LOCAL_NOTIFICATION=1`
  可关闭 CLI 本地通知

## 构建

```bash
make build      # 构建到 bin/atm
make install    # 构建并安装
make dist       # 跨平台编译（darwin/linux × amd64/arm64）
make clean      # 清理构建产物
```

## 更多文档

| 想知道 | 看 |
|---|---|
| 为什么这么设计、什么坚决不做 | [DESIGN.md](DESIGN.md) |
| 某个行为背后的机制、失败时会发生什么 | [docs/internals.md](docs/internals.md) |
| 某条命令的全部参数 | `atm <命令> --help` |
| 版本间的变化 | [CHANGELOG.md](CHANGELOG.md) |
| 当前可用性、支持矩阵、发布前清单 | [docs/release-readiness.md](docs/release-readiness.md) |
| 写一个连接器 / 额度源 | [连接器协议](docs/connector-protocol.md)、[额度源协议](docs/quota-provider-protocol.md) |
| 参与开发 | [CONTRIBUTING.md](CONTRIBUTING.md)、[开源边界](docs/open-source-boundary.md) |
