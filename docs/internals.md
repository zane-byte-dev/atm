# ATM 行为与机制

这里写**用起来会碰到、但从命令帮助里看不出来的行为**：状态怎么流转、失败时会发生什么、
数据存在哪儿、哪些保证是真的。

- 为什么这么设计、什么坚决不做 → [DESIGN.md](../DESIGN.md)
- 命令与参数 → `atm <命令> --help`
- 日常用法 → [README](../README.md)

## 目录

- [Todo 与执行](#todo-与执行)
- [外部收集](#外部收集)
- [内置文本模型服务](#内置文本模型服务)
- [会话数据](#会话数据)
- [通知与 hook](#通知与-hook)
- [存储与文件](#存储与文件)
- [App 与 CLI 的协议](#app-与-cli-的协议)

## Todo 与执行

### 生命周期

Todo 使用一套生命周期状态：`open/in_progress/waiting/review/blocked/done/dropped`。
`maintenance` 是标签而不是状态。当前会话通过 session↔todo 绑定表达焦点；待开始队列由 `open`
todo 的优先级和创建时间推导，不再单独保存 `focus/queued`。v4 的 `attention` 字段会在读取时自动
迁移，并在下次保存时移除；`atm now` 会在一个兼容版本内附带旧视图字段，支持 CLI 与 macOS App
分步升级。

Todo 离开 `in_progress` 时先关闭活跃 binding，再保存 Todo 状态：即使后续保存失败，也只会留下
可重新绑定的 `in_progress + unbound`，不会留下指向非工作态 Todo 的活跃 binding。
`atm todo show <id> --json` 中的 binding history 记录 `unbound_at` 与结构化 `reason`。

`atm todo archive <id>` 把已完结的 Todo 移出工作集：行仍然保留，所以它的 ID 不会被复用，
依赖和进展记录仍可引用它。面向日常删除使用 `atm todo trash <id>`：任何状态都能无确认移入
回收站，活跃 Session Binding 会安全关闭，但任务状态、Markdown、进展、依赖和历史都保留。
菜单栏 App 的普通删除走这条可恢复路径，只有回收站里的永久删除才要求确认。

### 依赖与唤醒

Todo 可通过 `depends_on` 建立结构化依赖。`atm todo done` 和批量完成会在同一次原子保存中检查
依赖图；当 waiting todo 的全部依赖都为 done 时，自动回到 open 并记录 wake 进展。依赖就绪只表示
工作可以开始，不表示实现已经提交 review。

`wake_condition` 中的 `waiting for todos: ...` 由完整 `depends_on` 集合确定性派生；重复添加同一
依赖不会产生重复边，新增或移除依赖会刷新派生文本。`atm todo wake` 是流水线、MR webhook 或人工
判断等外部条件的统一显式入口；`atm todo reconcile` 用于补偿执行并报告缺失、dropped 和循环依赖。

### link 与 creator

Todo link 以完整 `http/https` URL 为主标识，可选 `kind/title/relation` 元数据，不把 AOne、
GitHub、GitLab 等平台逻辑耦合进 ATM。同一 URL 重复添加时更新元数据，不会重复保存。带用户密码、
token、签名、credential 等敏感参数的 URL 会被拒绝。

`creator` 记录任务是谁建的，取值限定为 `me`、`collect` 或 agent 名，与自由文本 `source`
（为什么/从哪来）正交——因此 `atm todo list --creator collect` 是个能回答的问题。创建时自动判定：
环境里有 agent session 记该 agent，连接器收集记 `collect`，其余记 `me`；判定不准时用
`atm todo add --creator <值>` 显式声明。展示 `me` 时使用 `atm config set owner_name <昵称>`
配置的昵称（默认「我」），存储值始终是 `me`，改昵称不会改写任何记录。该字段自 schema v33 起存在，
更早创建的 todo 保持为空，不做回填。

### refine

`todo refine` 用 ATM [内置文本模型服务](#内置文本模型服务)把一条任务整理成可执行卡片：润色标题和
需求，复杂工作写计划，能独立关闭的部分拆成子任务并由父任务等待。它不启动 Agent。

每次整理会在任务分析中持久显示 `from <来源>`；「设置 → 模型」可编辑 `text_model_source` 来源
标识和 `todo_refine_prompt` 自定义指令。默认 Prompt 采用保守拆分策略：同一功能的分析、设计、
编码、测试和集成阶段保持在一张 Todo 中，只有可分别交付、验收和关闭的成果才拆为子任务。用户可
直接修改；Prompt 追加在 ATM 固定的安全、事实与 JSON 格式规则之后，留空恢复默认。

`todo add` 默认不整理；桌面添加在 `todo_refine_on_add`（默认开）时会自动跑一次。`in_progress`
的 Todo 只润色不拆分，避免把正在工作的会话解绑。

### prompt 与 handoff

`todo prompt` 输出一行交给人粘贴进新 Agent 会话的指针，不搬运需求本身：Agent 按指针自己去读
`todo doc`，拿到的永远是当前版本。想在会话之外补充需求就用 `todo log --section 补充`，它写进同一份
文档，接手的 Agent 一并读到。

`todo handoff` 在任务的工作目录里打开 Codex 并填好这行提示，不自动执行。

### run

`todo run` 是显式的本地执行入口，**只派发 Codex**（`todo agents` 显示本机是否已安装与费用说明）。
Codex 是唯一沙箱能被 ATM 强制、线程 id 能被 ATM 找回的 CLI，所以委派收敛到它一个：guarded 用
`codex exec --sandbox workspace-write` 并额外开放 ATM 数据目录，边界由 sandbox 而不是 prompt 保证。
全局 `--agent` 只是读过滤器（看哪个 Agent 的会话、任务与用量），传给 `todo run` 会被拒绝而不是
静默启动另一个 CLI。`--policy trusted` 绕过审批与 sandbox，必须显式传入并会输出警告。

派发 prompt 不要求 Agent 用 ATM 记录进展，只要求它把结果、证据和遗留问题写在最后一段回复里：这次
执行的会话本来就被索引成全文，Todo 详情直接读取会话最后一条回复，再写一遍等于把 ATM 已经拥有的
文字抄进第二张表。

它先创建唯一的 `task_runs` claim，再启动一个脱离调用终端的 ATM controller 来运行 Codex。同一 Todo
同时最多一个 starting/running Run；进程异常消失后，下次派发会把旧 claim 记为失败再重试。

退出语义是硬的：Agent 退出 0 只会把仍在进行中的 Todo 提交到 `review`，**永远不会自动 `done`**；
非零退出只记录 Run 失败，不改变 Todo 生命周期。`todo interrupt` 会停止 controller 及其 Agent
子进程树，将 Run 单独记为 `interrupted`，并让 Todo 保持 `in_progress`。子进程退出或被 interrupt
之后，controller 会向 App 发一条 `session_end`——它是唯一确知子进程已经没了的一方，否则 App 会
拿着一条「等待授权」信号直到安全 TTL 过期。每次派发的工作目录、策略、controller PID、时间、
退出码和日志路径均独立保存。

### --continue

已有执行关联到 Codex 线程后，可用 `--continue` 发送新的修改要求：ATM 创建新的 Run 审计记录（续跑的
意图记在 `resume_session_id`，与记录「这次执行实际是哪个会话」的 `session_id` 分开），并通过
`codex exec resume` 恢复原线程上下文。

传给 Codex 的必须是线程 UUID：Codex 会把认不出的 id 当成线程名，找不到时不报错而是静默新开一个
会话，所以 ATM 先把 `rollout-<时间>-<uuid>` 归一成 UUID，归一不出来就直接拒绝而不是假装续上。
`task_runs` 里 Claude、Grok、Pi 时代的行作为执行履历保留，但 `--continue` 只认 Codex 的会话：
它们的 session id 是 ATM 自己生成的，Codex 会把它当线程名静默新开一轮。

### context

`todo context` 是每次调用即时生成的只读快照，不代表 handoff 已持久化，也不触发 review 状态。
它默认使用 Todo 的单一活跃 Session 绑定；没有活跃绑定时退回最近绑定，多个活跃 worktree 时要求用
`--cwd` 明确选择。它区分 ATM 工作状态、Git 实现状态、历史里程碑与验证状态，列出 staged、unstaged
和 untracked 文件，但不会输出完整 diff、运行测试或改变 Todo 状态。`todo review-context` 暂时
保留为兼容别名。

## 外部收集

`collect` 是连接器采集面，不是 Agent 调度器。来源通过注册表按 `connector` 路由；连接器使用
[版本化 stdin/stdout JSON 协议](connector-protocol.md)，无需链接进 ATM。公开核心不内置服务专属
适配器。认证由连接器管理，ATM 不保存连接器 token/Cookie。采集默认关闭，启用前需在
`collection_connectors` 中配置并验证连接器。

macOS 菜单栏 App 常驻时按默认 5 分钟间隔采集；主窗口关闭不影响采集，退出 App 后停止。每个白名单
来源有独立 checkpoint，并重叠回读 20 分钟，消息 ID 与来源标记共同保证重复拉取不会重复新建。
语义匹配只用于在新 Todo 中记录相关历史 Todo，不会把新事项补充进旧 Todo。

`tasks` 来源可用 `--auto-dispatch`（App 中为「新 Todo 自动交给 Codex」）显式开启一次性派发，
默认关闭；`observe` 在存储层强制禁止派发。分类完成和 Agent 派发分别记账：Codex 启动失败时 Todo
仍保留，处理记录展示失败证据，可从 Todo 详情人工重试；成功 Run 仍只进入 `review`。自动派发来源
必须配置项目，工作目录只从这项可信配置解析（`~/mox/<project>`、`~/work/<project>` 或绝对路径），
不接受聊天或分类模型改写执行目录。模型只输出固定决策 JSON，TodoWriter 才能写 ATM；权限、模型或
写入失败会显示为等待重试且不会推进 checkpoint。

处理记录跟随它写出去的 Todo：那个 Todo 被完成或废弃后（在哪儿关的都算），这条记录一并了结，
折叠进「已保存与已了结」，主列表只留还欠一个动作的跟进。同一 Todo 的 `append` 不再平铺成同权重
记录，而是按时间收进对应 `create` 的详情。`insight` 先保存在记录自己的结论中，只有用户显式执行
`collect item save` 才写进中央知识库；`collect digest` 仅保留为人工的按来源/日期批量汇总入口。

`history` 和 `run` 拉到的聊天原文都会同步进 `~/.atm/atm.db`，默认保留 90 天：

```bash
atm config set collection_message_retention_days 30   # 改成 30 天
atm config set collection_message_retention_days 0    # 0 = 永久保留
```

## 内置文本模型服务

收集分类、日报沉淀和 `todo refine` 共用同一个内置文本模型服务：一次 schema 约束的 HTTP 调用，
没有 CLI、没有工具、没有沙箱和工作目录，因此聊天里的注入无处可去。

它直接调用 DeepSeek Chat Completions，默认模型是适合轻量文本职能的 `deepseek-v4-flash`，关闭
思考模式并要求 JSON 输出。API Key 保存在 `~/.atm/credentials.json`（目录 `0700`、文件 `0600`），
不进普通配置、备份、诊断包、argv 或日志：App 在「设置 → 模型」里填，CLI 用
`atm config credential set`（从 stdin 读）存同一份，`DEEPSEEK_API_KEY` 临时覆盖。模型和 endpoint
可在模型设置页修改，或通过 `text_model_name`、`text_model_base_url` 配置项覆盖；
`ATM_TEXT_MODEL_MODEL`、`ATM_TEXT_MODEL_BASE_URL` 适合临时调试。

用 `atm config test-text-model` 验证凭据和 endpoint。设置页的「测试连接」做同样的事：使用当前草稿值
发送一个最小 JSON 请求，不保存设置、也不读取或修改 Todo。

收集分类**失败即停**：模型不可用或答案读不出来时，这批消息保持未判定，checkpoint 不推进，下一轮
重建后重试，直到该记录的重试预算用完——绝不用关键词猜一个 Todo 塞进你的列表。`atm doctor` 会在
采集已启用但没有 Key 时先说出来，因为分类跑在后台，静默失败只会表现为「来源突然不产出了」。
ATM 仍会跳过早期 CLI 分类在 `atm-collection-model-*` 工作目录里留下的会话，它们不进
`atm session` 和 `atm stats`。

## 会话数据

### 数据质量

Parser 层自动处理以下噪音，保证存储和展示的数据是真实用户交互：

- IDE 系统标签（`<ide_opened_file>`、`<system-reminder>` 等）
- 中断标记（`[Request interrupted]`）
- Continuation session 重复消息（跨文件去重 + 同文件自身去重）
- Skill 前缀（`Base directory for this skill:`）

Parser 提取结构，不做业务判断——不提取 git commit、不生成摘要。

### 保留策略

索引比 Agent 自己的日志活得更久。

各家 Agent 都会轮转自己的日志（Claude Code 默认清理一个月前的 `~/.claude/projects`），所以
**源文件消失不等于会话被删除**：transcript 不在磁盘上之后，ATM 保留整条会话——正文、逐请求
token 和成本都还在，`atm stats --days 90` 的历史不会自己缩水。`atm doctor` 的 `retained=` 就是
这样被保留下来的会话数。唯一读不回来的是 `session show --thinking`（thinking 只存在于原始
transcript 里），这时会明确提示源文件已不在，而不是假装这个会话没有思考过程。

不想留的用 `atm session forget <id>` 永久移除，它的 token 和成本会一起离开所有统计。只有已保留的
会话能被 forget：源文件还在时下次 sync 会把它重新索引进来，所以命令会直接拒绝，而不是假装删掉。

索引**不做自动清理**：没有保留期，也没有后台 compaction，库单调增长是有意接受的成本。理由见
[DESIGN.md](../DESIGN.md)。删除只有 `session forget` 这一条显式路径；后续会加「推荐清理」：
ATM 列出候选和各自的 token/成本代价，由人确认后执行。

### 明细能力与统计口径

Claude、Codex、Pi 和 Qoder CLI 支持从上游 transcript 提供的字段读取逐请求模型与
input/output/cache token 明细；Pi 还支持会话增量同步、顺序消息和 thinking。同一会话切换模型时，
`atm stats --by model` 会分别归入实际模型；模型统计同时按 client（codex、claude、pi 等）区分，
同名模型不会跨 client 合并。`atm stats --by session` 仍展示会话汇总。各 Agent 的明细支持情况见
[DESIGN.md 的已知限制](../DESIGN.md)。

会话搜索使用固定字面子串，不依赖 FTS5。查询默认以只读快照打开数据库；`atm sync status --json`
会只读报告索引是否存在、最后成功同步时间、数据年龄、最近错误和已索引会话数。需要最新会话时再运行
`atm sync`，或给查询命令加 `--sync`。

### 项目归一

会话有可用 cwd 时，ATM 优先从 Git 根目录和 `origin` 推导 canonical project，因此不同目录名的
worktree 会自动合并；无法访问仓库路径的历史记录可用 `project_aliases` 显式归一：

```json
{
  "project_aliases": {
    "atm-memory-curator-iteration-1": "atm"
  }
}
```

统计会在读取时合并别名，因此不要求重写历史 SQLite 数据。

### session status 的三个正交状态

`session status --json` 将实时活动、显式 binding 和 Todo lifecycle 分开：`activity_state`
只表示近期是否观察到会话活动，`binding_state` 只表示 Session 是否显式绑定有效的
`in_progress` Todo，`todo.status` 仍是工作状态。相同项目或 `in_progress` 不能替代 binding；
没有实时活动的 binding 会单独保留，失效 binding 会显示为 `todo_missing` 或
`todo_not_in_progress`，不会静默伪装成未绑定。

### 会话记忆整理

会话记忆整理由 Agent 按 skill 执行，ATM 只提供确定性原语：`session list --review pending` 获取
未整理会话，`memory remember/supersede --source` 保存可追溯事实，`session review` 在整轮处理完成后
推进 append-only 整理游标。整理状态保存在 `~/.atm/memory/session-reviews.jsonl`，不会在 ATM
内部运行 LLM。

## 通知与 hook

通知**只在 hook 报出确切原因时触发**。不装 hook 的话，ATM 只能每 3 秒扫一遍会话记录去猜「这个
会话是不是在等你」，而这条路有两个躲不开的短板：会话记录是异步落盘的，而 Agent 卡在工具授权那一刻
**根本不会写下任何文字**——恰恰是最该提醒你的时刻反而看不见。所以那种关键词推测只计入菜单栏计数
和 Agent 页，不会弹通知。

跑完一轮不发通知——那不是被挡住，用提示音就够。Agent 继续往下走之后，已发出的通知自动撤回。

装 hook 时：

- 写入的是 Agent 自己的配置（`~/.claude/settings.json`、`~/.codex/hooks.json`、
  `~/.grok/hooks/atm-notch.json`、`~/.qoder/settings.json`），**只增删 ATM 自己那几条**，
  同一份配置里其他工具的 hook 一条不动
- 装的都是只上报的 hook（`SessionStart` / `UserPromptSubmit` / `Stop` / `SessionEnd` /
  `Notification`），**不会拦住工具调用，也不会替你做授权决定**；App 没在跑时 hook 立刻静默退出，
  不影响 Agent
- 事件走 `~/.atm/notch.sock`（0600），只在本机；带的是会话 ID、cwd 和一句提示文字。socket 与 hook
  文件沿用 `notch` 这个名字只是历史包袱（那套 UI 已经被通知取代），改名会让已装好的 hook 全部失效，
  因此保持不动
- **Grok Build** 使用独立文件 `~/.grok/hooks/atm-notch.json`，与同目录其他 hook 文件合并加载；
  payload 支持 Grok 的 camelCase 字段
- **Qoder** 的 hook 事件名与 payload 与 Claude 同构，一份 `~/.qoder/settings.json` 同时覆盖
  Qoder IDE 与 Qoder CLI。Qoder 只在启动时读这份配置，**装完要重启 Qoder 才生效**；ATM 因此不看
  「文件里装了」而只认真实收到的事件，重启前维持原来的关键词判断，收到第一个事件后自动切换
- **Pi** 没有 hook 配置文件，把 [`integrations/atm-notch.ts`](../integrations/atm-notch.ts) 复制到
  `~/.pi/agent/extensions/` 即可。Pi 的 `agent_settled` 上报为「已完成」而不是「需要你」——它分不清
  是做完了还是卡住了，而只有「需要你」会弹通知，猜错会让每一轮结束都弹一条
- 没接 hook 的 Agent（copilot / qoderwork）继续走关键词判断：仍会计入菜单栏的「需要你」计数，
  但不发通知

## 存储与文件

### 权限

`~/.atm` 建为 `0700`，`config.json` 与 `credentials.json` 写为 `0600`：这个目录里既有会话正文也有
一把 API Key，默认不该对同机其他用户可读，旧安装的宽权限在下一次写配置时被收紧。
`credentials.json` 权限比 `0600` 宽时 ATM 拒绝读取并提示 `chmod`，而不是照用一把可能已被别人
看过的 Key。

### SQLite

结构化观测数据、工作状态和连接器审计都存储在 `~/.atm/atm.db`（SQLite + WAL）。Todo、tag、依赖、
link、Session Binding 和 Comment 使用规范化表，状态与优先级枚举、日期格式由 CHECK 约束保证，
Comment 和 Binding 通过外键随 Todo 级联删除。写入统一走一个事务：先取写锁再读快照，因此不需要
乐观版本号；生命周期变更与 Binding 关闭/创建在同一次提交里落库。

### 日志

失败会落盘到 `~/.atm/logs/`：CLI 写 `cli.log`，菜单栏 App 写 `app.log`，一行一个 JSON 事件，
单文件封顶 5 MB 并保留一个轮转。**只记失败和进程启停**，不记会话正文、Todo/记忆/知识内容、凭据，
也不记命令参数（`atm todo add "<标题>"` 的标题本身就是内容），所以日志里是 `atm todo add` 而不是
完整命令行。App 无法在自己崩溃时写日志，因此用一个「上次是否正常退出」的标记来区分崩溃与正常退出，
并在日志里指向 macOS 的 crash report 目录而不是把报告内容抄进来。`atm diagnose --bundle` 会带上
两个日志的最后 200 行 —— 这是「每天失败一次、其余时间正常」这类间歇故障唯一能被看见的地方。

### backup / restore

`atm backup` 归档这个库真正无处重建的部分：Todo、共享记忆、中央知识、连接器收集账本和 review 游标。
会话镜像被有意排除——它由 `atm sync` 从各家 transcript 重建，归档因此小到一个量级，值得经常做。
排除的方式是清空而不是删表：恢复出来的库 schema 完整，`atm doctor` 立刻可读，下一次 sync 把行填回来。
`credentials.json` 也被有意排除，且不算作「未备份」的遗漏项：归档要经常做、要拷来拷去，不该顺手带走
一把还在用的 API Key。换机器后重新 `atm config credential set` 就行。

它同时是 schema 太旧被拒时的逃生口。`atm backup` 不走会 migrate 的打开路径，所以被
`minUpgradableVersion` 硬拒的库仍然备份得出来——先备份，再删库重建索引，这个顺序写在拒绝信息里。
`atm restore` 会拒绝比当前构建更新的归档（宁可不认，也不误读未知列），并把被替换的数据移到
`~/.atm/pre-restore-<时间>/` 而不是删除。

### Knowledge 的目录布局

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

搜索时传入 session ID 会记录召回，回答后用 feedback 标记采纳、纠正或拒绝；聚合质量分会温和影响
后续检索排序。`knowledge audit` 只输出重复、陈旧、源文件漂移和低质量建议，不自动归档。
`knowledge delete` 永久移出 ATM，导入源文件保留。

### Memory 与 Artifact

共享记忆以 append-only `~/.atm/memory/events.jsonl` 保存；artifact 原子写入
`~/.atm/artifacts/`。ATM 不在运行时探测其他产品的数据目录，历史数据应通过显式 importer 转换。
Knowledge、Memory 和 Artifact 只通过 ATM CLI 访问。

## App 与 CLI 的协议

macOS App 的周期刷新调用 `atm dashboard --json` 取会话索引快照，该协议当前为 v1，契约见
[`docs/contracts/dashboard-v1.schema.json`](contracts/dashboard-v1.schema.json)；并并发调用一次
`atm quota --json`。额度来自各 Agent 自己的日志而不是会话索引（Codex 会话 `rate_limits`、
Grok `~/.grok/logs/unified.jsonl` 里的 billing credits 与账期刷新时间）。私有或第三方额度源可通过
[`quota_providers` 版本化命令协议](quota-provider-protocol.md)提供通用多指标卡片，无需链接进 ATM
或暴露服务凭据。额度不进快照协议——这样 CLI 与 App 的版本可以继续各自演进，任一额度源读取失败也
只影响自己的卡片。
