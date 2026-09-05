# ATM — AI Team Manager

给人用的 AI 管理面板。看 AI 都在干什么、干得怎么样、花了多少钱。

本地优先、单机单用户：它读各家 AI CLI 已经写在你硬盘上的会话记录，索引成一个能查、能统计、
能挂待办的库。日常主界面是本机浏览器：Web 提供任务、收集、Agent、知识、统计、AI Day 和设置七个工作区，
Go 服务负责后台同步、采集、Hook 与通知路由；菜单栏和全局语音是可选的独立 App。它不代理你和 AI 的对话，
停掉它也不影响任何会话。

ATM 会索引可能包含源码、提示词、模型回复和个人信息的本地数据。公开使用或参与开发前请读
[隐私与数据处理](PRIVACY.md)、[安全报告流程](SECURITY.md)和[贡献指南](CONTRIBUTING.md)；
提交 issue、日志或导出前请先脱敏。

## 安装

**一键安装（推荐，无需 Go）**：

```bash
curl -fsSL https://raw.githubusercontent.com/zane-byte-dev/atm/main/install.sh | sh
```

**go install（仅 CLI，无需 Node.js）**：

```bash
go install github.com/zane-byte-dev/atm/cmd/atm@latest
```

此方式不包含编译后的 Web 页面；启动新 Web 实例需要完整构建。CLI 不依赖 Web 服务运行。

**从源码构建完整版**（需要 Go 1.25+、Node.js 24 和 npm）：

```bash
make install    # npm ci、构建 Web、嵌入 Go，再安装到 /usr/local/bin
```

完整构建和后续发布包把页面嵌入同一个 `atm` 二进制，运行时无需 Node.js。本次 Web 迁移尚未发版，
体验当前工作区请从源码构建。当前构建只接受当前 schema 基线；旧库不能靠运行时兼容分支直接打开。

## 上手

装完不用配置，直接跑——数据源路径会自动检测：

```bash
atm doctor              # 认出了哪些 AI 工具、明细覆盖率如何
atm session status      # 现在各个 AI 在干什么
atm now                 # 我这边工作中、等待中、待验收的事
atm stats --days 7      # 这周花了多少 token 和钱
```

## 本地 Web 工作区

使用完整构建启动：

```bash
atm serve --open         # 前台运行服务，并打开浏览器工作区
atm serve status --json  # 在另一个终端查看实例
atm serve stop           # 停止同一数据目录的实例
```

默认只监听 `127.0.0.1:47321`，通过 `--port` 更换端口，`--port 0` 自动分配。再次运行
`atm serve --open` 会打开已有实例。首次打开或浏览器会话到期后，使用这条命令建立浏览器会话；Go 服务
重启后，有效会话会通过本地持久签名密钥继续工作。直接输入未授权的地址不会获得业务数据。服务在终端前台运行，关闭浏览器不会停止它，`Ctrl-C` 或 `serve stop`
才会退出。

Web 提供任务生命周期、计划/依赖/等待、图片上传、AI 整理，知识文档原地编辑与共享记忆、采集来源管理、
会话检索、用量与 AI Day，以及业务设置和五种主题。任务与知识草稿按编辑器隔离保存在浏览器，关闭后可显式恢复；
并发编辑有版本检查，创建与后台执行保留幂等身份。页面通过按域 SSE 更新，CLI 变化在有订阅时每 2 秒检查。

`serve` 负责自动同步、按配置采集、AI Day、Agent Hook 和通知路由；状态每 8 秒回补，
关闭网页或退出菜单栏都不会停止后台。开发和验收实例同样会运行这些职责，因此必须使用独立数据目录。
手动同步、采集、重建、额度刷新和 AI 整理通过有记录的后台执行完成，可查看结果或取消。

macOS 可使用用户级登录服务：

```sh
atm serve install --print       # 预览配置
atm serve install              # 安装并启动当前完整二进制
atm serve --open                # 打开已运行实例
atm serve uninstall            # 取消登录服务，保留数据
```

[ATM Menu](app/menubar/README.md) 使用普通 macOS 菜单，展示服务状态、今日 Token、当前任务与缓存额度摘要，并提供 Web、新增任务、同步及自身设置入口。[ATM Voice](app/voice/README.md) 独立提供全局语音，
不依赖 Go 服务。构建步骤、旧偏好导入和测试边界见 [Web 开发说明](app/web/README.md)；架构见
[技术方案](docs/design/local-web-runtime.md)。代码已实现这些入口，真实日用切换和新 App 的系统权限验收需另外完成。

**现有数据**：当前构建创建并只接受 schema v57。历史迁移梯子已删除；旧库需先使用仍支持其 schema
的历史版本升级，或按错误提示先备份不可重建数据、重建当前数据库。当前 `atm serve migrate` 仅供以后
明确注册的单阶迁移使用，在 v57 基线上是无操作，不会猜测或跨越任意旧版本。

自定义数据目录在每条 `serve` 命令中传相同的 `--data-dir`。新建空工作区直接创建当前基线。旧版二进制
不能打开较新的库；回退前停止写入并另存新数据。`serve stop` 会卸载当前登录会话的托管 job、保留 plist；
`serve uninstall` 才移除登录配置，两者都保留业务数据与 Go presence 所有权记录。

## 命令

完整命令与参数一律看 `atm <命令> --help`（比如 `atm todo handoff --help`）——那里是唯一不会过期的
真相。下面只列日常最常用的：

**看 AI 在干什么**（别名 `atm s`）

```bash
atm session status                      # 实时状态
atm session list --days 7               # 最近活动，默认最多 200 条；--offset 翻页
atm session search <关键词>              # 全文搜索历史；生成调用也可用 --query
atm session show <session-id>           # 完整 Q/A，--thinking 带思考过程
atm session clip <关键词>                # 复制 AI 回复到剪贴板
```

**待办**

```bash
atm todo add "<标题>"                    # 加一条；--refine 立刻润色并按需拆分
atm todo add "<标题>" --image a.png       # 附本地图片；--image 可重复，最多 10 张
atm todo list                           # 看列表
atm todo start <id>                     # 进入工作中
atm todo start <id> --reopen-reason "验收后为什么恢复" # 重开 review/done
atm todo handoff <id>                   # 在 Codex Desktop 打开并填好指针，不按回车
atm todo handoff <id> --copy            # 只复制那行指针，粘贴进新的 Agent 会话
atm todo plan set [id] --file -         # 原子替换结构化执行计划快照
atm todo submit <id> --reason "实现及证据" # Agent 完成实现后提交待确认
atm todo done <id> --reason "验收证据"     # 仅由人验收完成
atm todo archive <id>                   # 归档（可恢复，保留生命周期与历史）
atm todo restore <id>                   # 从归档恢复
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
atm day rebuild --from 2026-08-01 --to 2026-08-15 # 显式重建损坏或过期的投影
atm day badge code_architect --json               # 诊断单枚徽章的进度和证据
atm day sources list --json                       # 检查来源权限与衍生事件数
atm day export > ai-day.json                      # 导出全部衍生数据（不含原文）
```

AI Day 在本机把会话镜像归一化为不含原文的事件，按最近 30 个有效日生成每日唯一结果。完整的数据模型、
12 枚徽章、评分规则与隐私行为见 [AI Day 说明](docs/ai-day.md)。Web 提供日常查看和显式重建；
公开 CLI 保留人工修复、来源诊断和数据导出。更细的纠正与隐私管理仍可参考该说明，不把每个内部动作开放成 Web API。

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
atm collect enable                       # 开启 Go 服务常驻期间的后台自动收集
atm collect run                          # 立即增量收集一次（也是登录失效后恢复的方式）
atm collect item save <item-id>          # 确认后把收集结论保存为知识
atm collect item read <item-id>          # 标记一条收集结果已读
atm collect item read --all              # 全部标为已读
atm collect item unread <item-id>        # 重新标为未读
atm collect item archive <item-id>       # 了结记录（保留审计和关联 Todo）
atm collect item archive --all           # 批量了结已读、未保存进知识库的结论
atm collect item unarchive <item-id>     # 重新打开已了结记录
atm collect source mute <source-id>      # 这个来源不再弹通知（照常收集、照常算未读）
atm collect source unmute <source-id>    # 恢复通知
```

连接器的登录过期后，ATM 会停手而不是每个周期重试一遍：后台对这个连接器静默 30 分钟，
同一轮里第一个来源失败就跳过它的兄弟来源（它们共用刚失败的那份凭证）。连接器可以在
`~/.atm/config.json` 里声明 `login_command`，CLI 会把它打印在状态行上，收集页面显示登录问题。
ATM 自己不会执行登录命令，登录由人完成后再重试采集。

**外发动作闸门**（默认不安装）

```bash
atm guard install dws --bin ~/.qoderwork/bin/dws   # 装到工具自己的路径上
atm guard status                         # 哪些工具被拦住了，哪些被绕过了
atm guard list                           # 待授权的外发动作
atm guard show <id>                      # 一条请求的全文与执行结果
atm guard approve <id>                   # 批准，并由 ATM 执行这条命令
atm guard deny <id> --reason "内容不对"    # 拒绝
atm guard uninstall dws                  # 撤掉，把工具的二进制放回原位

atm guard rule list                      # 有哪些规则、哪些关着、哪些是内置的
echo '{"id":"doc-write","label":"写钉钉文档","path":["doc","write"]}' \
  | atm guard rule set dws               # 注册一条（规则走 stdin，因为它是嵌套对象）
echo '{"id":"mr-remind","enabled":false}' | atm guard rule set a1   # 关掉一条内置规则
atm guard rule remove dws doc-write      # 删掉自己加的
atm guard forget mytool                  # 忘掉一个注册过的 CLI（先 uninstall）
```

当前 Web 不开放 Guard 审批或工具安装；不在 PATH 上的 CLI（比如 `dws`）用 `--bin` 给出绝对路径。

拦的是「会被别人看到」的动作——发钉钉消息、催 MR 评审人、推 ATA 群——读操作完全不碰。装闸门之前
先 `atm backup`，装完跑一次 `atm guard status`：**PATH 上有同名两份时装错那份等于没装**，`status`
会明确告诉你。规则在 `~/.atm/config.json` 的 `guard` 段，不配置也有内置的三条。
机制、取舍与拦不到什么见 [docs/internals.md](docs/internals.md#外发动作闸门)。

全局 flag：

- `--agent` — 按 agent 过滤：`claude`、`codex`、`pi`、`copilot`、`qoder`、`qodercli`、`qoderwork`、`grokbuild`、`antigravity`
- `--json` — JSON 输出（list、search、status、show、stats 等均支持）
- `--sync` — 查询前显式同步会话源；查询默认只读，不会修改数据库

## 可选 macOS App

主工作区运行在浏览器。两个独立原生工具按需构建和使用：

```sh
app/menubar/Scripts/build-app.sh
open "app/menubar/dist/ATM Menu.app"
app/voice/Scripts/build-app.sh
open "app/voice/dist/ATM Voice.app"
```

[ATM Menu](app/menubar/README.md) 只连接 Go 的有界本机控制接口。点击状态项打开普通系统菜单；
菜单展示服务状态、今日 Token、当前任务和缓存额度摘要，把详情跳转到 Web，也可要求 Go 排队同步。它不直接扫描数据库、同步会话或运行采集，也不提供 Guard 决策入口。[ATM Voice](app/voice/README.md) 管理自己的快捷键、模型与权限，
退出 ATM 服务后仍可使用。各自提供旧偏好的白名单导入，新 bundle 的系统权限必须实际授权。

旧 [`app/macos`](app/macos/README.md) 主界面源码只作为历史实现参考保留，已有性能修复不删除；
新产品的构建不依赖它，当前 Go 也不再提供它所需的运行时或 IPC 接口。不要把它作为当前数据目录的客户端运行。

## 通知

Agent 卡住等你时，Go 记录需要关注的状态，并交给可用显示渠道。ATM Menu 获得通知权限后负责稳定 ID 的通知显示和撤回；
Agent 继续执行时更新状态。没有伴随 App 时可降级为普通本机横幅，备用横幅不保证撤回。

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
Web 不提供 Hook 安装或权限审批，安装状态通过上述 CLI 检查。

[外发动作闸门](#命令)的通知是另一类：它**自带「批准并发送 / 拒绝」两个按钮**，可以直接在通知上做决定，
不用打开窗口；快速面板顶部也会列出待授权的那几条，带正文和同样两个按钮。请求被决定或过期后通知自动撤回。
这类通知不依赖 hook（闸门自己就是那条命令），但依赖系统通知权限——如果你之前拒过 ATM 的通知权限，
就只剩快速面板这条路。

## Agent 集成

把 ATM 作为 prompt/skill 挂到你的 AI agent，让 agent 直接帮你查状态、管待办：

- **pi**：把 [`integrations/pi-prompt.md`](integrations/pi-prompt.md) 复制到
  `~/.pi/agent/prompts/atm.md`；可将 [`integrations/pi-atm-attention.ts`](integrations/pi-atm-attention.ts)
  安装到 `~/.pi/agent/extensions/`，在每个会话首次执行前只注入当前绑定或最多 3 条仓库候选
- **Codex**：把 [`integrations/codex-agents.md`](integrations/codex-agents.md) 的 ATM 段落合入
  `~/.codex/AGENTS.md`；SessionStart hook 可调用 [`integrations/codex-atm-context.sh`](integrations/codex-atm-context.sh)，
  避免把完整 `atm now --json` 反复塞入上下文。Agent 完成实现使用 `todo submit`，`todo done` 留给人的验收
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
- `session list` 与 JSON 格式的 `session export` 统一输出包含 `schema_version`、总数和分页信息的对象；
  流式处理使用显式的 JSONL 导出格式
- 时间戳使用 ISO 8601 格式（`2026-06-24T10:05:25+08:00`）

## 平台能力

- macOS / Linux 支持默认数据源路径自动检测；所有数据源路径可通过 `~/.atm/config.json` 覆盖
- `atm session clip` 支持 macOS `pbcopy`、Linux `wl-copy`/`xclip`/`xsel`、Windows `clip`
- todo 人向通知包括新建、待验收和完成；`--json` 同样发送，通知失败不影响任务状态。
  已由 Go 接管时 CLI 转交同一个通知渠道，Menu 按稳定 ID 显示；未接管的独立 CLI 使用 macOS
  `terminal-notifier`/`osascript` 或 Linux `notify-send`。设 `ATM_SKIP_LOCAL_NOTIFICATION=1` 可关闭 CLI 通知。
- 外发动作闸门只支持 macOS / Linux：它靠 `exec` 替换当前进程，装的 shim 也是 POSIX shell 脚本。
  Windows 上 `atm guard install` 直接报错而不是装一个半能用的东西
- 想再加一层保险，可以在 `~/.claude/settings.json` 的 `permissions.deny` 里加
  `"Bash(*-atm-real*)"`，挡住直接调用被移开的真身。**这一条要你自己加，ATM 不会代写**——
  `atm agent hook install` 写的是同一个文件，而它承诺过只装上报 hook、不改任何权限决定

## 构建

```bash
make build      # 安装前端依赖、编译页面，构建完整 bin/atm
make build-cli  # 只构建 CLI 到 bin/atm，无需 Node.js
make web-build  # 仅构建 app/web/dist
make install    # 完整构建并安装
make dist       # 嵌入页面后跨平台编译（darwin/linux × amd64/arm64）
make clean      # 清理二进制和前端构建产物
```

`build` 与 `build-cli` 默认写入同一产物，按需选择其中一种。开发验证使用仓库内的独立目录，保留
可执行文件名 `atm`，既不覆盖正在使用的 CLI，也兼容按可执行文件名授权的本机安全策略：

macOS 构建会优先使用钥匙串中的 Apple Development 身份，避免本机安全策略拦截临时签名；没有开发
证书时回退到 ad hoc 签名。需要固定身份时设置 `ATM_CODESIGN_IDENTITY`，CLI 与两个当前 App 构建脚本使用
同一选择规则。

```bash
NPM_CONFIG_CACHE=/private/tmp/atm-web-npm-cache make build BIN_DIR=bin/atm-web
bin/atm-web/atm serve --data-dir /private/tmp/atm-web-data --port 0 --open
```

调整页面时可启用 Go 同源的 Vite 热更新。在一个终端运行 `npm run dev --prefix app/web`，另一个
终端运行：

```sh
mkdir -p bin/atm-web-dev
go build -o bin/atm-web-dev/atm ./cmd/atm
./scripts/codesign-local.sh bin/atm-web-dev/atm
bin/atm-web-dev/atm serve --data-dir /private/tmp/atm-web-dev-data --port 47322 --dev-ui http://127.0.0.1:5173 --open
```

浏览器使用 Go 打开的地址；页面和 HMR 经过同源代理，API 鉴权保持生效。开发模式无需预先构建 `dist`。
发布前仍需运行完整构建，且不传 `--dev-ui`。详见 [Web 开发说明](app/web/README.md#go-同源热更新)。

`--data-dir` 只用于 `serve` 及其子命令，不修改 HOME；写入测试应使用空目录或脱敏副本。上述 npm 缓存
路径也适用于默认缓存存在权限问题的开发环境。GoReleaser 会先执行同一套前端安装和构建，再用 `webui`
标签嵌入页面；未生成页面时完整构建失败，不能发布缺少界面的包。

## 更多文档

| 想知道 | 看 |
|---|---|
| 为什么这么设计、什么坚决不做 | [DESIGN.md](DESIGN.md) |
| 本地 Web、后台接管与原生应用拆分的方向 | [本地 Web 技术方案](docs/design/local-web-runtime.md) |
| 某个行为背后的机制、失败时会发生什么 | [docs/internals.md](docs/internals.md) |
| 某条命令的全部参数 | `atm <命令> --help` |
| 版本间的变化 | [CHANGELOG.md](CHANGELOG.md) |
| 当前可用性、支持矩阵、发布前清单 | [docs/release-readiness.md](docs/release-readiness.md) |
| 写一个连接器 / 额度源 | [连接器协议](docs/connector-protocol.md)、[额度源协议](docs/quota-provider-protocol.md) |
| 参与开发 | [CONTRIBUTING.md](CONTRIBUTING.md)、[开源边界](docs/open-source-boundary.md) |
