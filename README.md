# ATM — AI Team Manager

给人用的 AI 管理面板。看 AI 都在干什么、干得怎么样、花了多少钱。

> 设计原则与非目标见 [DESIGN.md](DESIGN.md)
> 版本间的变化见 [CHANGELOG.md](CHANGELOG.md)
> 当前可用性、支持矩阵和发布前清单见 [docs/release-readiness.md](docs/release-readiness.md)

公开使用或参与开发前，请阅读 [隐私与数据处理](PRIVACY.md)、
[安全报告流程](SECURITY.md) 和 [贡献指南](CONTRIBUTING.md)。ATM 会索引可能包含源码、
提示词、模型回复和个人信息的本地数据；提交 issue、日志或导出前请先脱敏。

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

### macOS 菜单栏

独立菜单栏 App 常驻显示今日 Token；主窗口提供任务、收集、Agent、知识和用量工作区。Agent 工作区侧栏分
「活跃 / 全部」：活跃是实时窗口内的会话，全部走持久索引并按开始时间倒序分页，会话滑出实时窗口后仍然点得到。
会话详情的「对话」提供三段式阅读——摘要（最近几轮）、时序（消息与模型请求交错，含每次请求的用量）、
完整（全部轮次，并尽可能带上思考过程）；思考读不到时会区分「文件已被 Agent 轮转掉」和「这个 Agent 不存
思考正文」，而不是都渲染成空白。收集工作区
把钉钉等外部来源的消息自动分类为新 Todo、知识沉淀或忽略记录，并提供失败重试、纠错与撤销；与历史 Todo 有关时在新 Todo 中记录关联，不合并事项。
处理记录跟随它写出去的 Todo：那个 Todo 被完成或废弃后（在哪儿关的都算），这条记录一并
了结，折叠进「沉淀与已了结」，主列表只留还欠一个动作的跟进。
沉淀的内容按来源每天汇总成一篇知识文档写进中央知识库。
知识工作区
按“知识库 / 文章 / 详情”组织中央知识，并把共享记忆作为特殊知识库统一浏览。支持跨库搜索、
新建和导入 Markdown、编辑元数据、移动知识库，以及可恢复的归档。

```bash
app/macos/Scripts/build-app.sh
open app/macos/dist/ATM.app
```

开发与路径配置见 [`app/macos/README.md`](app/macos/README.md)。

## 命令

```bash
# 会话 (别名: atm s)
atm session status  [--agent X]                          # AI 工具实时状态
atm session list    [--agent X] [--days N] [--project P] # 最近会话列表
atm session list    --all --order desc --limit 200 [--offset N]  # 整个索引，按开始时间倒序分页
atm session search  <keyword>  [--agent X]               # 全文搜索会话历史
atm session show    <session-id> [--thinking]            # 查看完整 Q/A（可含模型思考过程）
                                                         # Codex/Grok/Pi 有思考正文；Claude Code 只存签名，会明说
atm session timeline <session-id>                        # 消息与模型请求时间线
atm session clip    <keyword>  [--agent X]               # 复制 AI 回复到剪贴板
atm session export  [--format json|csv] [--days N]       # 导出原始数据
atm session forget  <session-id> [-y]                    # 永久移除源文件已消失的会话

# 待办与工作状态
atm now                                     # 工作中、等待中、待验收、阻塞和到期复查
atm dashboard --json                       # 一次返回带 schema_version 的桌面聚合快照
atm todo    [list|add|start|run|runs|tail|submit|done|drop|trash|restore|show|context|edit|move|log|doc|prompt]
atm todo start <id>                         # 进入工作中；done/dropped 会重新开始
atm todo context [id] --json                # 临时、只读汇总 Todo、Session 与 Git 上下文
atm todo submit [id] --reason "实现及证据"   # 显式提交待确认，不直接标记 done
atm todo wait <id> --wake "条件"             # 进入等待中，直到条件变化
atm todo wait <id> --review-at 2026-07-17   # 到期后进入需处理
atm todo maintain <id> --limit 3            # 有边界的维护批次
atm todo link add <id> <url> --kind cr      # 关联完整 URL（CR/MR/流水线/文档等）
atm todo link list <id>                     # 查看关联链接
atm todo link remove <id> <url>             # 移除关联链接
atm todo depend add <id> <dependency-id>    # <id> 等待 <dependency-id>；依赖完成后回到 open
atm todo depend list <id>                   # 查看依赖满足状态
atm todo wake <id> --reason "流水线完成"     # 外部事件/人工条件显式唤醒
atm todo reconcile                          # 补偿唤醒并审计缺失、放弃、循环依赖
atm todo bulk done <id>... --reason "完成"  # 批量完成（也支持 drop/move/edit）
atm todo prompt <id> --copy                 # 复制一行启动提示，粘贴进新的 Agent 会话
atm todo run <id> [--cwd /path/to/repo]      # 后台派发；默认 Codex、默认 guarded
atm todo run <id> --agent claude             # 派发给 Claude Code（也支持 grokbuild / pi）
atm todo agents                              # 查看可派发的 Agent、是否已安装与费用说明
atm todo run <id> --continue "修改要求"       # 恢复该 Agent 最近一次会话并继续修改
atm todo runs <id>                           # 查看每次派发的状态、PID 与退出结果
atm todo interrupt <id>                      # 中断当前 Agent 进程树，Todo 保持工作中
atm todo tail <id> [-f] [--bytes N]          # 查看/持续跟随最近一次派发日志，可限制最近 N 字节
atm todo match --prompt                 # 启动时只给当前仓库最多 3 个候选
atm session bind <id>                   # 当前 agent 会话接手 TODO，并自动 start
atm session current                     # 查看当前会话绑定
atm todo log <id> "补充：..." --section 补充   # 从任务外补充需求，Agent 接手时会读到
atm todo log <id> "结果：...；证据：...；下一步：..." # 单段、最多 400 字的里程碑动态
atm todo log <id> "详细设计..." --section 分析          # 调研/架构细节不进入动态
atm todo lint <id>                      # 检查冗长动态、无效 tID 和文档状态漂移

# 新增与删除的脚本友好接口
atm todo add "<title>" --desc-file <path>  # - 表示从 stdin 读取多行描述
atm todo add --batch                       # 从 YAML/JSON stdin 批量创建；示例见 --help
id=$(atm todo add "<title>")               # 非 JSON 模式 stdout 仅输出新 ID
atm todo trash <id>                        # 移到回收站，不确认、可恢复
atm todo list --status trashed             # 查看回收站（archived 仍是兼容别名）
atm todo restore <id>                      # 从回收站恢复原状态
atm todo delete <id> -y                    # 永久删除；默认确认，通常只在回收站使用

# 报告与系统
atm report  [date]     [--agent X]               # 每日活动报告
atm stats   [--agent X] [--days N] [--by model|model-day|model-hour|skill|session|request|speed|day|hour] # 使用统计
atm stats   --by skill [--agent X] [--days N]     # Skill 调用次数、会话数与 Agent 数
atm stats   --by request [--session ID]          # 单次模型请求明细
atm stats   --by speed [--days N]                # 模型输出速度 tok/s 与轮次等待时长
atm doctor                                          # 数据源与明细覆盖率诊断（含可测速比例）
atm diagnose [--bundle] [-o path]                # 报障用支持包：版本/schema/doctor 结论/同步错误/日志尾部，脱敏且不联网
atm sync    [--agent X]                          # 手动触发数据同步
atm sync status [--agent X] --json               # 只读查看索引新鲜度、最近同步结果与错误
atm config  [init]                               # 查看/初始化配置文件
atm backup  [-o path]                            # 归档无处重建的记录（todo/记忆/知识/收集账本）
atm restore <archive> [--yes]                    # 从归档恢复；被替换的数据移到 pre-restore-<时间>/

# 外部需求收集（可扩展连接器）
atm collect status --json                         # 健康状态、来源、运行和处理记录
                                                  # 文本里的 Filed Todos 说明建过多少、还开着几个；
                                                  # JSON 每条记录带 todo_status/todo_archived
atm collect source search deploy --connector slack --kind channel --limit 10
atm collect source add --connector slack --kind channel --id C123 --name deploys
atm collect source add --connector github --kind issue --id owner/repo#42 --project atm
atm collect source add --connector slack --kind channel --id C123 --project atm --auto-dispatch
atm collect source add --connector slack --kind channel --id C456 --strategy observe --interval 60
atm collect source add --connector slack --kind channel --id C456 --knowledge-collection inbox
atm collect source add --connector slack --kind channel --id C456 --instruction "只关注 MR 和需求"
atm collect history <source-id> [--limit 50] [--since 2026-07-28]
                                                  # 看来源原文并同步到本地；不产生 Todo
atm collect history <source-id> --local           # 只读已同步的部分，不打网络
atm collect search  <关键词> [--source X] [--sender X] [--since D] [--limit 20]  # 搜本地已同步的聊天
atm collect enable                                # 开启 App 常驻期间的后台自动收集
atm collect run [--source <source-id>]            # 立即增量收集一次
atm collect run --due                             # 后台模式：只运行达到各来源独立间隔的来源
atm collect digest [--source X] [--date 2026-08-03]  # 把当天沉淀汇总成一篇知识文档（同一天重跑原地重写）
atm collect digest --dry-run                      # 先看摘要内容，不写知识库
atm collect digest --due                          # 后台模式：距上次沉淀不足 60 分钟就跳过
atm collect item reprocess <item-id>               # 重新判断失败、已忽略或已沉淀记录
atm collect item promote <item-id> [--title X]     # 将忽略/沉淀/失败记录显式转成 Todo
atm collect item correct <item-id> [--title X] [--project X] [--priority P1]
atm collect item revert <item-id> -y               # 撤销误创建/误补充，保留审计轨迹
atm collect item delete <item-id>... -y            # 删除处理记录本身；它写出的 Todo 保留（多个 id 一个事务，用于清空一整组）
# history 和 run 拉到的聊天原文都会同步进 ~/.atm/atm.db，默认保留 90 天：
#   atm config set collection_message_retention_days 30   # 改成 30 天
#   atm config set collection_message_retention_days 0    # 0 = 永久保留
# 分类模型可写成候选链，前一个限额/超时/没装就自动换下一个：
#   atm config set collection_model_command "grok,codex"

# 中央知识、共享记忆和产物
atm knowledge catalog
atm knowledge collection list
atm knowledge collection create <id> [--name X] [--description X] [--role X] [--topic X]
atm knowledge collection edit <id> [--name X] [--use-when X] [--instruction X]
atm knowledge collection rename <id> <new-id>
atm knowledge collection delete <id> [--force | --move-to X]
atm knowledge list [--collection X] [--limit N] [--offset N]
atm knowledge search <query> [--collection X] [--domain X] [--tag X] [--status active] [--session ID]
atm knowledge get <document-id>
atm knowledge add <title> --file note.md [--collection inbox]
atm knowledge import <file-or-directory> [--collection research]
atm knowledge update <document-id> --file note.md
atm knowledge edit <document-id> [--title X] [--collection X] [--status active|draft|archived]
atm knowledge delete <document-id>                  # 永久移出 ATM；导入源文件保留
atm knowledge feedback <document-id> --session ID --outcome adopted|corrected|rejected [--note X]
atm knowledge quality [document-id]
atm knowledge quality --issues-only --summary
atm knowledge bulk-edit <document-id>... [--collection X] [--status archived]
atm knowledge audit [--stale-days 180]
atm knowledge doctor
atm memory recall [query] [--scope project:mox]
atm memory remember <content> [--scope global] [--tag architecture]
atm memory supersede <memory-id> <content> [--scope project:mox]
atm memory supersede <memory-id> --file note.md [--scope project:mox]
atm memory forget <memory-id> [--scope project:mox]
atm artifact save <title> --file report.md
```

`todo prompt` 输出一行交给人粘贴进新 Agent 会话的指针，不搬运需求本身：Agent 按指针自己去读
`todo doc`，拿到的永远是当前版本。想在会话之外补充需求就用 `todo log --section 补充`，它写进同一份
文档，接手的 Agent 一并读到。

`todo run` 是显式的本地执行入口，默认派发 Codex，`--agent` 可选 `claude`、`grokbuild`、`pi`
（`todo agents` 列出本机是否已安装、费用与安全说明）。它先创建唯一的 `task_runs` claim，再启动一个脱离
调用终端的 ATM controller，由 controller 以受限权限运行所选 Agent。受限的形式取决于 Agent 自己提供的
能力：Codex 是 `workspace-write` sandbox 并额外开放 ATM 数据目录；Grok 是 workspace sandbox，权限模式
用 `auto` 并额外放行 `atm` 命令——它的 `acceptEdits` 只自动放行文件编辑，其余工具仍会发起审批请求，
无人应答的无头执行会把整轮取消，`auto` 则只把被拦截的调用回报给模型。派发 prompt 不要求 Agent 用 ATM
记录进展，只要求它把结果、证据和遗留问题写在最后一段回复里：这次执行的会话本来就被索引成全文，Todo 详情
直接读取会话最后一条回复，再写一遍等于把 ATM 已经拥有的文字抄进第二张表，还要 Agent 额外花轮次；Grok
没有 `--add-dir` 等价开关，workspace sandbox 下 `~/.atm` 只读，这类写入本来也不会成功。会话绑定由
controller 在派发前完成。Claude Code 没有可由 ATM 强制的文件系统 sandbox，guarded 用
它自己的权限规则——工作目录内的编辑自动放行、其余工具在非交互下按未授权拒绝，只额外放行 `atm` 命令；
Pi 两者都没有，因此只能以 trusted 运行。`--policy trusted` 会绕过所选 Agent 的审批与 sandbox，必须
显式传入并会输出警告。同一 Todo 同时最多一个 starting/running Run；进程异常消失后，下次派发会把旧 claim
记为失败再重试。Agent 退出 0 只会把仍在进行中的 Todo 提交到 `review`，永远不会自动 `done`；非零退出只记录
Run 失败，不改变 Todo 生命周期。退出码不是唯一证据：Grok 在整轮被取消时同样退出 0，ATM 解析它的终局
`end` 事件，只有 `stopReason=end_turn` 才算完成，否则记为失败且不提交 review。`todo interrupt` 会停止 controller 及其 Agent 子进程树，将 Run 单独记为
`interrupted`，并让 Todo 保持 `in_progress`，之后仍可重新派发或继续已有会话。每次派发的工作目录、策略、
controller PID、时间、退出码和日志路径均独立保存。
已有执行关联到 Codex 线程后，可用 `--continue` 发送新的修改要求：ATM 创建新的 Run 审计记录（续跑的意图记在
`resume_session_id`，与记录「这次执行实际是哪个会话」的 `session_id` 分开），并通过 `codex exec resume`
恢复原线程上下文。传给 Codex 的必须是线程 UUID：Codex 会把认不出的 id 当成线程名，找不到时不报错而是静默新开
一个会话，所以 ATM 先把 `rollout-<时间>-<uuid>` 归一成 UUID，归一不出来就直接拒绝而不是假装续上。
Claude Code、Grok 和 Pi 接受 ATM 生成的 session id，派发前就把会话绑定到 Todo，续跑直接沿用同一个 id，
不需要事后认领。`--continue` 只续跑同一个 Agent 最近一次留下会话的执行。
macOS 任务详情提供“交给 Agent”、实时状态、失败重试与“继续修改”输入框；来源开启自动派发时也复用完全相同的入口。

`collect` 是连接器采集面，不是 Agent 调度器。来源通过注册表按 `connector` 路由；连接器使用
[版本化 stdin/stdout JSON 协议](docs/connector-protocol.md)，无需链接进 ATM。公开核心不内置服务专属适配器。
macOS 菜单栏 App 常驻时按默认 5 分钟间隔采集；主窗口关闭不影响采集，退出 App 后停止。每个白名单来源有独立
checkpoint，并重叠回读 20 分钟，消息 ID 与来源标记共同保证重复拉取不会重复新建。语义匹配只用于在新 Todo 中记录相关历史 Todo，不会把新事项补充进旧 Todo。认证由连接器管理，ATM 不保存连接器 token/Cookie。
`tasks` 来源可用 `--auto-dispatch`（App 中为“新 Todo 自动交给 Codex”）显式开启一次性派发，默认关闭；
`observe` 在存储层强制禁止派发。分类完成和 Agent 派发分别记账：Codex 启动失败时 Todo 仍保留，处理记录展示
失败证据，可从 Todo 详情人工重试；成功 Run 仍只进入 `review`。自动派发来源必须配置项目，工作目录只从这项
可信配置解析（`~/mox/<project>`、`~/work/<project>` 或绝对路径），不接受聊天或分类模型改写执行目录。
模型只输出固定决策 JSON，TodoWriter 才能写 ATM；权限、模型或写入失败会显示为等待重试且不会推进 checkpoint。
采集默认关闭，启用前需在 `collection_connectors` 中配置并验证连接器。

分类模型由 `collection_model_command` 决定，可以写成按序尝试的候选链
（`atm config set collection_model_command "grok,codex"`）：前一个被限额、超时或没装时自动换下一个，
一个 CLI 挂掉不再整条采集停摆。内置 `codex` 与 `grok` 两套无头调用方式（都在一次性工作目录里跑、只读沙箱、
禁联网搜索/记忆/子 Agent；grok 没有 `--ignore-user-config` 等价开关，差异见文档）；其他 CLI 通过
[`collection_model_runners` 模板](docs/collection-model-runner.md)自行声明 argv 和取值路径，不用改代码。
链尾可以放 `rule` 作为最后兜底，降级产生的记录会写明原因。ATM 会跳过分类自己在
`atm-collection-model-*` 工作目录里留下的 CLI 会话，它们不进 `atm session` 和 `atm stats`。

`todo context` 是每次调用即时生成的只读快照，不代表 handoff 已持久化，也不触发 review 状态。
它默认使用 Todo 的单一活跃 Session 绑定；没有活跃绑定时退回最近绑定，
多个活跃 worktree 时要求用 `--cwd` 明确选择。它区分 ATM 工作状态、Git 实现状态、历史里程碑与验证状态，
列出 staged、unstaged 和 untracked 文件，但不会输出完整 diff、运行测试或改变 Todo 状态。
`todo review-context` 暂时保留为兼容别名。

`session status --json` 将实时活动、显式 binding 和 Todo lifecycle 分开：`activity_state`
只表示近期是否观察到会话活动，`binding_state` 只表示 Session 是否显式绑定有效的
`in_progress` Todo，`todo.status` 仍是工作状态。相同项目或 `in_progress` 不能替代 binding；
没有实时活动的 binding 会单独保留，失效 binding 会显示为 `todo_missing` 或
`todo_not_in_progress`，不会静默伪装成未绑定。

全局 flag：
- `--agent` — 按 agent 过滤：`claude`、`codex`、`pi`、`copilot`、`qoder`、`qodercli`、`qoderwork`
- `--json` — JSON 格式输出（支持 list、search、status、show、stats）
- `--sync` — 在查询前显式同步会话源；查询默认只读，不会修改数据库

## 数据质量

Parser 层自动处理以下噪音，保证存储和展示的数据是真实用户交互：
- IDE 系统标签（`<ide_opened_file>`、`<system-reminder>` 等）
- 中断标记（`[Request interrupted]`）
- Continuation session 重复消息（跨文件去重 + 同文件自身去重）
- Skill 前缀（`Base directory for this skill:`）

## AI Agent 集成

把 ATM 作为 prompt/skill 挂到你的 AI agent，让 agent 直接帮你查状态、管待办：

- **pi**：把 [`integrations/pi-prompt.md`](integrations/pi-prompt.md) 复制到 `~/.pi/agent/prompts/atm.md`；可将 [`integrations/pi-atm-attention.ts`](integrations/pi-atm-attention.ts) 安装到 `~/.pi/agent/extensions/`，在每个会话首次执行前只注入当前绑定或最多 3 条仓库候选
- **Codex**：SessionStart hook 可调用 [`integrations/codex-atm-context.sh`](integrations/codex-atm-context.sh)，避免把完整 `atm now --json` 反复塞入上下文
- **其他 agent**（Claude Code 等）：以该文件内容作为 prompt/skill 参考

## Agent 事件推送与通知

Agent 卡住等你时，ATM 发一条系统通知，点击直接跳到它所在的终端；Agent 继续往下走之后通知自动撤回。菜单栏同时显示「需要你 N」，不看通知也能一眼扫到。跑完一轮不发通知——那不是被挡住，用提示音就够。

通知**只在 hook 报出确切原因时触发**。不装 hook 的话，ATM 只能每 3 秒扫一遍会话记录去猜「这个会话是不是在等你」，而这条路有两个躲不开的短板：会话记录是异步落盘的，而 Agent 卡在工具授权那一刻**根本不会写下任何文字**——恰恰是最该提醒你的时刻反而看不见。所以那种关键词推测只计入菜单栏计数和 Agent 页，不会弹通知。

装上 hook 后，Agent 会在事件发生时直接推给 ATM：

```bash
atm agent hook install            # Claude / Codex / Grok Build / Qoder 都装；Pi 见下方说明
atm agent hook install --source claude
atm agent hook install --source qoder
atm agent hook status             # 看当前接了哪些事件
atm agent hook uninstall          # 原样摘掉
```

- 写入的是 Agent 自己的配置（`~/.claude/settings.json`、`~/.codex/hooks.json`、`~/.grok/hooks/atm-notch.json`、`~/.qoder/settings.json`），**只增删 ATM 自己那几条**，同一份配置里其他工具的 hook 一条不动
- 装的都是只上报的 hook（`SessionStart` / `UserPromptSubmit` / `Stop` / `SessionEnd` / `Notification`），**不会拦住工具调用，也不会替你做授权决定**；App 没在跑时 hook 立刻静默退出，不影响 Agent
- 事件走 `~/.atm/notch.sock`（0600），只在本机；带的是会话 ID、cwd 和一句提示文字。socket 与 hook 文件沿用 `notch` 这个名字只是历史包袱（那套 UI 已经被通知取代），改名会让已装好的 hook 全部失效，因此保持不动
- **Grok Build** 使用独立文件 `~/.grok/hooks/atm-notch.json`，与同目录其他 hook 文件合并加载；payload 支持 Grok 的 camelCase 字段
- **Qoder** 的 hook 事件名与 payload 与 Claude 同构，一份 `~/.qoder/settings.json` 同时覆盖 Qoder IDE 与 Qoder CLI。Qoder 只在启动时读这份配置，**装完要重启 Qoder 才生效**；ATM 因此不看「文件里装了」而只认真实收到的事件，重启前维持原来的关键词判断，收到第一个事件后自动切换
- **Pi** 没有 hook 配置文件，把 [`integrations/atm-notch.ts`](integrations/atm-notch.ts) 复制到 `~/.pi/agent/extensions/` 即可。Pi 的 `agent_settled` 上报为「已完成」而不是「需要你」——它分不清是做完了还是卡住了，而只有「需要你」会弹通知，猜错会让每一轮结束都弹一条
- 没接 hook 的 Agent（copilot / qoderwork）继续走关键词判断：仍会计入菜单栏的「需要你」计数，但不发通知

也可以在 App 的「设置 → 通知」里一键安装并查看接入状态，以及开关通知本身。

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

各家 Agent 都会轮转自己的日志（Claude Code 默认清理一个月前的 `~/.claude/projects`），所以
**源文件消失不等于会话被删除**：transcript 不在磁盘上之后，ATM 保留整条会话——正文、逐请求
token 和成本都还在，`atm stats --days 90` 的历史不会自己缩水。索引因此比 Agent 自己的日志活得更久，
`atm doctor` 的 `retained=` 就是这样被保留下来的会话数。唯一读不回来的是 `session show --thinking`
（thinking 只存在于原始 transcript 里），这时会明确提示源文件已不在，而不是假装这个会话没有思考过程。

不想留的用 `atm session forget <id>` 永久移除，它的 token 和成本会一起离开所有统计。只有已保留的
会话能被 forget：源文件还在时下次 sync 会把它重新索引进来，所以命令会直接拒绝，而不是假装删掉。

索引**不做自动清理**：没有保留期，也没有后台 compaction，库单调增长是有意接受的成本。原因写在
[DESIGN.md](DESIGN.md) 里——它比 Agent 自己的日志活得更久正是它的意义，而「哪条会话不再需要」只有人知道。
删除只有 `session forget` 这一条显式路径；后续会加「推荐清理」：ATM 列出候选和各自的 token/成本代价，
由人确认后执行。

Claude、Codex、Pi 和 Qoder CLI 支持从上游 transcript 提供的字段读取逐请求模型与
input/output/cache token 明细；Pi 还支持
会话增量同步、顺序消息和 thinking。同一会话切换模型时，`atm stats --by model`
会分别归入实际模型；模型统计同时按 client（codex、claude、pi 等）区分，
同名模型不会跨 client 合并。`atm stats --by session` 仍展示会话汇总。

结构化观测数据、工作状态和连接器审计都存储在 `~/.atm/atm.db`（SQLite + WAL）。Todo、tag、依赖、link、
Session Binding 和 Comment 使用规范化表，状态与优先级枚举、日期格式由 CHECK 约束保证，
Comment 和 Binding 通过外键随 Todo 级联删除。写入统一走一个事务：先取写锁再读快照，
因此不需要乐观版本号；生命周期变更与 Binding 关闭/创建在同一次提交里落库。

`atm todo archive <id>` 把已完结的 Todo 移出工作集：行仍然保留，所以它的 ID 不会被复用，
依赖和进展记录仍可引用它；`atm todo list --status archived` 查看，`atm todo unarchive` 取回。
面向日常删除使用 `atm todo trash <id>`：任何状态都能无确认移入回收站，活跃 Session Binding
会安全关闭，但任务状态、Markdown、进展、依赖和历史都保留；`atm todo restore <id>` 恢复。
菜单栏 App 的普通删除走这条可恢复路径，只有回收站里的永久删除才要求确认。

失败会落盘到 `~/.atm/logs/`：CLI 写 `cli.log`，菜单栏 App 写 `app.log`，一行一个 JSON 事件，
单文件封顶 5 MB 并保留一个轮转。**只记失败和进程启停**，不记会话正文、Todo/记忆/知识内容、凭据，
也不记命令参数（`atm todo add "<标题>"` 的标题本身就是内容），所以日志里是 `atm todo add` 而不是完整命令行。
App 无法在自己崩溃时写日志，因此用一个「上次是否正常退出」的标记来区分崩溃与正常退出，并在日志里
指向 macOS 的 crash report 目录而不是把报告内容抄进来。`atm diagnose --bundle` 会带上两个日志的
最后 200 行 —— 这是「每天失败一次、其余时间正常」这类间歇故障唯一能被看见的地方。

`atm backup` 归档这个库真正无处重建的部分：Todo、共享记忆、中央知识、连接器收集账本和 review 游标。
会话镜像被有意排除——它由 `atm sync` 从各家 transcript 重建，归档因此小到一个量级，值得经常做。
排除的方式是清空而不是删表：恢复出来的库 schema 完整，`atm doctor` 立刻可读，下一次 sync 把行填回来。

它同时是 schema 太旧被拒时的逃生口。`atm backup` 不走会 migrate 的打开路径，所以被
`minUpgradableVersion` 硬拒的库仍然备份得出来——先备份，再删库重建索引，这个顺序写在拒绝信息里。
`atm restore` 会拒绝比当前构建更新的归档（宁可不认，也不误读未知列），并把被替换的数据移到
`~/.atm/pre-restore-<时间>/` 而不是删除。

会话搜索使用固定字面子串，不依赖 FTS5。查询默认以只读快照打开数据库；`atm sync status --json`
会只读报告索引是否存在、最后成功同步时间、数据年龄、最近错误和已索引会话数。需要最新会话时再运行
`atm sync`，或给查询命令加 `--sync`。macOS App 的周期刷新调用 `atm dashboard --json` 取会话索引快照，该协议当前为 v1，契约见
[`docs/contracts/dashboard-v1.schema.json`](docs/contracts/dashboard-v1.schema.json)；并并发调用一次
`atm quota --json`。额度来自各 Agent 自己的日志而不是会话索引（Codex 会话
`rate_limits`、Grok `~/.grok/logs/unified.jsonl` 里的 billing credits 与账期刷新时间）。
私有或第三方额度源可通过 [`quota_providers` 版本化命令协议](docs/quota-provider-protocol.md)
提供通用多指标卡片，无需链接进 ATM 或暴露服务凭据。额度不进快照协议——这样 CLI 与 App 的版本可以
继续各自演进，任一额度源读取失败也只影响自己的卡片。

Todo 使用一套生命周期状态：`open/in_progress/waiting/review/blocked/done/dropped`。
`maintenance` 是标签而不是状态。当前会话通过 session↔todo
绑定表达焦点；待开始队列由 `open` todo 的优先级和创建时间推导，不再单独保存
`focus/queued`。v4 的 `attention` 字段会在读取时自动迁移，并在下次保存时移除；`atm now`
会在一个兼容版本内附带旧视图字段，支持 CLI 与 macOS App 分步升级。

`creator` 记录任务是谁建的，取值限定为 `me`、`collect` 或 agent 名，与自由文本 `source`
（为什么/从哪来）正交——因此 `atm todo list --creator collect` 是个能回答的问题。创建时自动判定：
环境里有 agent session 记该 agent，连接器收集记 `collect`，其余记 `me`；判定不准时用
`atm todo add --creator <值>` 显式声明。展示 `me` 时使用 `atm config set owner_name <昵称>`
配置的昵称（默认「我」），存储值始终是 `me`，改昵称不会改写任何记录。该字段自 schema v33 起存在，
更早创建的 todo 保持为空，不做回填。

Todo 可通过 `depends_on` 建立结构化依赖。`atm todo done` 和批量完成会在同一次原子保存中检查
依赖图；当 waiting todo 的全部依赖都为 done 时，自动回到 open 并记录 wake 进展。依赖就绪只表示
工作可以开始，不表示实现已经提交 review。
`wake_condition` 中的 `waiting for todos: ...` 由完整 `depends_on` 集合确定性派生；重复添加同一依赖
不会产生重复边，新增或移除依赖会刷新派生文本。Todo 离开 `in_progress` 时先关闭活跃 binding，
再保存 Todo 状态：即使后续保存失败，也只会留下可重新绑定的 `in_progress + unbound`，不会留下
指向非工作态 Todo 的活跃 binding。`atm todo show <id> --json` 中的 binding history 记录
`unbound_at` 与结构化 `reason`，可用于迁移审计。
`atm todo wake` 是流水线、MR webhook 或人工判断等外部条件的统一显式入口；
`atm todo reconcile` 用于补偿执行并报告缺失、dropped 和循环依赖。

Todo link 以完整 `http/https` URL 为主标识，可选 `kind/title/relation` 元数据，不把
AOne、GitHub、GitLab 等平台逻辑耦合进 ATM。同一 URL 重复添加时更新元数据，不会重复保存。
带用户密码、token、签名、credential 等敏感参数的 URL 会被拒绝。

Knowledge 位于 `~/.atm/knowledge/`，顶层目录就是可读、可直接编辑的 collection。
例如 `ink/` 保存个人经历，`xifeng/` 保存经济与策略文章，未分类内容进入 `inbox/`。
每个 collection 可用 `_collection.md` 描述用途、主题和路由协议；除正文说明外，可声明
`role`、`useWhen`、`avoidWhen` 和 `instructions`。`knowledge catalog` 会结构化返回这些字段
和文档数量。模型先 catalog 判断集合并遵守协议，再用 collection-scoped search 找片段，
最后按稳定 document ID 执行 get。稳定 ID 保存在 YAML frontmatter 中，不进入文件名。
Knowledge 搜索每次直接读取 Markdown：这个量级下全量解析约 20ms，比进程启动本身还便宜，
没有任何缓存或索引需要失效。Markdown 始终是唯一事实源。

Collection 完全由目录决定，文档 frontmatter 不保存 collection。`knowledge collection`
命令组只进行目录与 `_collection.md` manifest 操作：rename 直接移动目录，文档自动随目录改库；
删除非空库必须显式 `--force`，或用 `--move-to` 先迁移文档。只有根目录文档、没有实体目录的
历史 inbox 在 rename 时也会被聚合到新目录。

外部 Markdown 只有通过显式 `knowledge add/import` 才会进入；collection 表示知识来源和整体用途，
domain、tag 和 project 是跨 collection 的查询 metadata。目录导入保留源相对路径；指定
`--collection` 时写入对应集合。Markdown 修改后 search/get 立即读取最新内容，无需重建索引。

共享记忆以 append-only `~/.atm/memory/events.jsonl` 保存；artifact 原子写入
`~/.atm/artifacts/`。ATM 不在运行时探测其他产品的数据目录，历史数据应通过显式 importer 转换。

会话记忆整理由 Agent 按 skill 执行，ATM 只提供确定性原语：`session list --review pending` 获取未整理会话，`memory remember/supersede --source` 保存可追溯事实，`session review` 在整轮处理完成后推进 append-only 整理游标。整理状态保存在 `~/.atm/memory/session-reviews.jsonl`，不会在 ATM 内部运行 LLM。

Knowledge、Memory 和 Artifact 只通过 ATM CLI 访问。搜索时传入 session ID 会记录召回，
回答后用 feedback 标记采纳、纠正或拒绝；聚合质量分会温和影响后续检索排序。
`knowledge audit` 只输出重复、陈旧、源文件漂移和低质量建议，不自动归档。

ATM 不提供 Agent Schedule、HTTP、Webhook、独立 daemon/serve 或 MCP 服务。Agent 定时任务属于运行 Agent 的客户端；
Enchanted 只在应用打开且 Mac 唤醒期间执行，错过的 occurrence 不补跑。ATM 代管显式发起的 `todo run`，以及
来源明确开启 `auto_dispatch` 后对新 Todo 的一次性同路径派发；它不是 Agent 自循环，失败不会自动无限重启。
需要人在可见会话里工作时仍使用 `todo prompt`。

```json
{
  "project_aliases": {
    "atm-memory-curator-iteration-1": "atm"
  }
}
```

会话有可用 cwd 时，ATM 优先从 Git 根目录和 `origin` 推导 canonical project，因此不同目录名的
worktree 会自动合并；无法访问仓库路径的历史记录可用 `project_aliases` 显式归一。统计会在读取时
合并别名，因此不要求重写历史 SQLite 数据。

## 定价

stats 的费用按模型定价计算。可在 `~/.atm/config.json` 的 `pricing` 字段覆盖或新增模型，每项为 `[input, output, cache_create, cache_read]`（美元/百万 token）：

```json
{
  "pricing": {
    "claude-opus-4-6": [15.0, 75.0, 18.75, 1.50]
  }
}
```

未配置的模型回退到内置默认价。

## 输出

- stdout 给机器读（JSON），stderr 给人读（sync 进度）
- `--json` 模式下 stdout 是纯净 JSON，可直接 pipe 到 `jq`
- 空列表统一输出 `[]`，不会输出 `null`
- 时间戳使用 ISO 8601 格式（`2026-06-24T10:05:25+08:00`）

## 平台能力

- macOS / Linux 支持默认数据源路径自动检测；所有数据源路径可通过 `~/.atm/config.json` 覆盖
- `atm session clip` 支持 macOS `pbcopy`、Linux `wl-copy`/`xclip`/`xsel`、Windows `clip`
- todo 人向通知支持 macOS `terminal-notifier`/`osascript` 和 Linux `notify-send`：新建、待验收（submit/review）、完成、放弃都会提醒；`--json` 同样发送；缺少通知命令时静默跳过，不影响任务状态。菜单栏 App 在刷新时也会对外部新建/进入待验收发原生通知。设 `ATM_SKIP_LOCAL_NOTIFICATION=1` 可关闭 CLI 本地通知

## 构建

```bash
make build      # 构建到 bin/atm
make install    # 构建并安装
make dist       # 跨平台编译（darwin/linux × amd64/arm64）
make clean      # 清理构建产物
```
