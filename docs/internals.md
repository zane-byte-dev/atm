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
- [外发动作闸门](#外发动作闸门)
- [存储与文件](#存储与文件)
- [App 与 CLI 的协议](#app-与-cli-的协议)

## Todo 与执行

### 生命周期

Todo 使用四个生命周期状态：`open/in_progress/review/done`。
`maintenance` 是标签而不是状态。当前会话通过 session↔todo 绑定表达焦点；待开始队列由 `open`
todo 的优先级和创建时间推导，不再单独保存 `focus/queued`。v4 的 `attention` 字段会在读取时自动
迁移，并在下次保存时移除；`atm now` 会在一个兼容版本内附带旧视图字段，支持 CLI 与 macOS App
分步升级。

Todo 离开 `in_progress` 时，关闭活跃 binding 与保存 Todo 状态在同一个 SQLite 事务里提交；任一步
失败都会整体回滚，不会留下指向非工作态 Todo 的活跃 binding，也不会出现状态已改但 binding 未关。
`atm todo show <id> --json` 中的 binding history 记录 `unbound_at` 与结构化 `reason`。

`todo submit` 和兼容命令 `todo wait` 还会在这个事务里写入 `work_effect_outbox`。后者只写
in_progress 的等待样式元数据并解绑，不会产生 waiting 状态。Markdown 日志、Todo 文档
元数据和桌面通知只有在事务提交后才执行；adapter executor 成功后由 Work service ack 对应 outbox 行。进程若在 commit
后退出，或文件写入失败，下一次 Submit/Wait 会拿到同一个 effect ID 继续补偿；即使用省略 ID 的绑定
命令重试，也会从该 Session 最近关闭的 binding 恢复目标，但只有关闭原因、Todo 当前状态和 pending
effect 三者一致时才会采用，旧 binding 不会被误当成当前任务。

这套投递保证是 **at-least-once**，不是 exactly-once：若进程在外部副作用成功后、ack 提交前崩溃，
重试可能再次写日志或发送通知。文档元数据同步本身是收敛覆盖；通知是提示性质。outbox 保证的是
成功的生命周期变更不会永久丢掉后续动作，而不是跨 SQLite、文件系统和操作系统通知做分布式事务。

`todo plan set` 在同一 Work application service 中写 `todo_plan_revisions`，但不改 Todo 生命周期。
每次写入是整份不可变 JSON 快照，按 Todo 单调递增 revision；`base_revision` 防止两个 Agent 静默覆盖，
与最新快照完全相同的重试则先于冲突检查直接幂等返回。revision 同时记录 session、binding、run、agent
和 request provenance。最新计划会进入 `todo show/context --json`，并投影到 Todo Markdown 的生成区块；
投影失败可通过重试同一快照或再次读取 `todo doc` 修复。Plan step 不是 Todo，全部 completed 也不会自动
submit 或 done。

`waiting` 不是生命周期：`in_progress` 带 `wake_condition` 或 `review_at` 时，客户端可显示橙色等待样式，
但它仍在工作中分组。Schema v51 将历史 waiting/blocked 迁为 in_progress；“暂不处理”迁回 open。

`atm todo archive <id>` 可把任意阶段 Todo 移出工作集：行仍然保留，所以 ID 不会被复用，生命周期、
依赖、Markdown 和进展记录也都保留；活跃 Session Binding 会安全关闭。`trash/drop` 是 archive 的
兼容别名，`unarchive` 是 restore 的兼容别名。Schema v51 将历史 dropped 迁为归档的 open。
菜单栏 App 只有“归档/恢复”，永久删除只从归档视图提供。

### 依赖与唤醒

Todo 可通过 `depends_on` 建立结构化依赖。`atm todo done` 和批量完成会在同一次原子保存中检查
依赖图。带未满足依赖的 open Todo 保持在待办并拒绝 start/bind；已经开始的 Todo 保持 in_progress，
用派生 wake_condition 显示等待样式，依赖全部 done 后只清除等待元数据。依赖就绪只表示工作可以继续，
不表示实现已经提交 review。

`wake_condition` 中的 `waiting for todos: ...` 由完整 `depends_on` 集合确定性派生；重复添加同一
依赖不会产生重复边，新增或移除依赖会刷新派生文本。`atm todo wake` 是流水线、MR webhook 或人工
判断等外部条件的兼容清除入口；`atm todo reconcile` 用于补偿执行并报告缺失与循环依赖。

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

`todo add` 默认不整理；桌面上优化是详情页动作栏的一个按钮，弹窗里可以带一句本次要求（CLI 是 `--hint`），
只有打开 `todo_refine_on_add`（默认关）时桌面添加才会自动跑一次。`in_progress`
的 Todo 只润色不拆分，避免把正在工作的会话解绑。

### handoff

`todo handoff` 在任务的工作目录里打开 Codex 并填好一行指针，不自动执行。指针不搬运需求本身：
Agent 按它自己去读 `todo doc`，拿到的永远是当前版本。想在会话之外补充需求就用
`todo log --section 补充`，它写进同一份文档，接手的 Agent 一并读到。

只要那行文字就用 `todo handoff --copy`：复制并打印指针，不打开任何窗口。它连工作目录都不解析，
因为目录只对深链有意义——为了复制一行字而让一个绑在多个 worktree 上的 Todo 报错，是只有真要打开
Codex 的调用方才需要回答的问题。这个分支就是过去的 `todo prompt`。

ATM 不在后台启动 Agent：没有 `todo run`、没有收集器自动派发。会话开始于人在 Codex 里按回车，
ATM 通过 `session bind` 认识它。

### context

`todo context` 是每次调用即时生成的只读快照，不代表 handoff 已持久化，也不触发 review 状态。
它默认使用 Todo 的单一活跃 Session 绑定；没有活跃绑定时退回最近绑定，多个活跃 worktree 时要求用
`--cwd` 明确选择。它区分 ATM 工作状态、Git 实现状态、历史里程碑与验证状态，列出 staged、unstaged
和 untracked 文件，但不会输出完整 diff、运行测试或改变 Todo 状态。曾经存在的兼容别名
`todo review-context` 已删除：它和 `todo context` 逐字相同，而名字里的 review 会让人以为它会
推进 review 状态。

## 外部收集

`collect` 是连接器采集面，不是 Agent 调度器。来源通过注册表按 `connector` 路由；连接器使用
[版本化 stdin/stdout JSON 协议](connector-protocol.md)，无需链接进 ATM。公开核心不内置服务专属
适配器。认证由连接器管理，ATM 不保存连接器 token/Cookie。采集默认关闭，启用前需在
`collection_connectors` 中配置并验证连接器。

macOS 菜单栏 App 常驻时按默认 5 分钟间隔采集；主窗口关闭不影响采集，退出 App 后停止。每个白名单
来源有独立 checkpoint，并重叠回读 20 分钟，消息 ID 与来源标记共同保证重复拉取不会重复新建。
语义匹配只用于在新 Todo 中记录相关历史 Todo，不会把新事项补充进旧 Todo。

收集器只创建或补充 Todo，不启动 Agent。模型只输出固定决策 JSON，TodoWriter 才能写 ATM；权限、模型或
写入失败会显示为等待重试且不会推进 checkpoint。

连接器登录过期是唯一按连接器停手的失败：后台路径静默这个连接器 30 分钟，同一轮里第一个来源失败
就跳过它的兄弟来源——它们共用刚失败的那份凭证，再试只是把同一条错误多写几遍。手工 `collect run`
永远照跑，那是人登录完之后的恢复入口。缺少权限照旧报出来但不停手：实测它是单来源的（某个会话
读不到，别的来源照常），按连接器停会因为一个坏来源把好的一起停掉。连接器可在
`collection_connectors.<id>.login_command` 里声明重新登录的命令；ATM 从不自己执行它（登录要人扫码），
只在 CLI 状态行里打印、在 App 横幅和一条（每次失效仅一条）通知上给出按钮。

判定按话题走（同一会话、间隔 15 分钟内为一批，通知流来源按单条消息），一批一个决策——一小时里
一条值得记的决策和五十句玩笑必须能分别回答。但人读到的不该是话题数：**一轮收集的 `insight` 在
收尾时合成一条记录**，标题正文由内置模型合成（模型不可用时按各条原文拼接，判定原因里说明），
被合并的分条直接删除，消息 ID 并进合成条，所以那批聊天仍算已处理、20 分钟回读不会重收。原始消息
留在 `collection_messages`，合成条的 `raw_context` 也保留这批聊天原文。`create`/`append` 不参与合并
——一件活儿仍是一条记录；`ignore` 也不合并，它本来就折叠在「已保存与已了结」。运行计数里
`analyzed` 记判了几个话题（花了几次模型调用），`insight` 记看得见的记录条数。按需的
`collect analyze` 不合并：那是人工审阅入口，提议要能逐条确认。

处理记录跟随它写出去的 Todo：那个 Todo 被完成或废弃后（在哪儿关的都算），这条记录一并了结，
折叠进「已保存与已了结」，主列表只留还欠一个动作的跟进。同一 Todo 的 `append` 不再平铺成同权重
记录，而是按时间收进对应 `create` 的详情。`insight` 先保存在记录自己的结论中，只有用户显式执行
`collect item save` 才写进中央知识库；`collect digest` 仅保留为人工的按来源/日期批量汇总入口。
没有关联 Todo、或不想等待 Todo 生命周期时，可用 `collect item archive` 手动了结记录；它只写
`collection_items.archived_at`，不会改 Todo、不会删除审计，也不会释放消息让下一轮重复收集。
`collect item unarchive` 可恢复到主列表；App 中对应「了结记录」和「重新打开」。

`collect item archive --all` 是同一个动作的批量版，只扫一类记录：**已读、且没保存进知识库的
结论**。这类记录是唯一没有自己的生命周期可跟随的——`create`/`append` 由 Todo 关掉时一并了结，
`ignore` 从来不是活儿，而一条结论「看过了，不值得进知识库」这个结论本身没有落点，于是主列表
只会涨。已读在这里就是那个决定：打开过又没保存，就是答复。所以还欠动作的一律扫不到——未读结论、
Todo 还开着的跟进、重试用尽的失败，都得点名 ID。批量只会关不会开：`all` 只能配 `archived:true`，
批量重新打开不存在。它也不会顺手把记录标成已读——已读是入选前提，写它就等于让这个动作自己制造
入选资格。`collect status` 报可了结条数，App 记录页在「全部已读」旁边多一个「全部了结」按钮，
只在真有可了结记录时出现。判定只有 Go 一份（`store.ArchiveSettledCollectionItems`），App 不重算。

新产生的收集结果默认未读：新建 Todo、对已有 Todo 的补充、还没保存的结论和等人确认的提议都算，
`collect status` 报未读数，侧栏和菜单栏出徽标，App 前台时还会弹一条桌面通知。打开即已读，
`collect item read --all` 全部标已读，`collect item unread` 重新标未读。同一来源后续追加会让它重新变成未读。

三个开关管三件不同的事，别混：`collect disable` 关掉整体自动调度；`collect source disable`
暂停某个来源的采集；`collect source mute` 只掐这个来源的桌面通知——它照常采集、结果照常计入未读、
徽标照常涨，只是不再弹窗。所以「不想被打断」用 mute，「不想再看这个群」才用 disable。
静默状态由 `mute`/`unmute` 独占：`collect source add` 是 upsert，编辑来源不会顺手把静默改回来。
命令行里静默来源在 `collect source list` 有单独一行标注，`collect status` 报静默来源数；App 里
来源行有 🔇 标记、详情页有「桌面通知」开关。找不到来源的历史 Run 仍然通知——「查不到」不等于「已静默」。

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
  不影响 Agent。想拦住动作的是另一套东西，走的是完全相反的契约，见
  [外发动作闸门](#外发动作闸门)
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

## 外发动作闸门

上一节说 ATM 只上报、不替你做授权决定。这一节看着像反转，其实不是：两者都是同一条更深的不变量的
推论。

> **ATM 绝不为别人已经在问的问题准备第二个答案。**

hook 路径观察的是**已经存在**的决策界面——Agent 自己的授权提示。那里再放一个应答方，就变成两个提示
抢一个决定，所以只上报。闸门路径**自己就是那条命令**：驱动这些 CLI 的技能文档给出的命令都带 `-y`
（`dingtalk/SKILL.md`、`mr/SKILL.md`），明确关掉了 CLI 自己的二次确认，没有竞争的提示可抢。ATM 不是
加第二个答案，是把技能拿掉的那唯一一个还回来。

两套东西的契约正好相反，不能混用：`atm agent hook` 跑在 Agent 的回合里、必须透明（永远 exit 0、
stdout 永远为空）；`atm guard exec` **就是**那条命令，它的全部意义就是能说不，是 ATM 里唯一允许
非 0 非 1 退出的地方。闸门也**不通过 hook 机制安装**。

**`-y` 这个前提是承重的，而且检测不了。** 将来技能去掉 `-y`，CLI 自己的提示就回来了，又变成两个
提示抢一个决定。加规则前请确认这一点。

### 机制

`atm guard install <tool>` 把工具的二进制移到同目录的 `<name>-atm-real`，原位置放一个三行 shim
转发到 `atm guard exec`。插桩点默认取 **PATH 解析到的那一份**——机器上常有同名两份（`a1` 和
`aone-kit` 在 `~/.local/bin` 和 `~/.qoderwork/bin` 各有一份，而后者不在 PATH 上），装错那份等于
没装却显示成功，所以 `guard status` 把 `shadowed`（PATH 先找到别的同名文件）和 `clobbered`
（shim 被覆盖）分开报，两者都会让 `atm doctor` 出警告。

### 注册与管理

规则可以用 `atm guard rule set <tool>`（从 stdin 读一个 JSON 对象）或 App 的「设置 → 外发闸门」增删。
规则走 stdin 而不是 flag：它是个嵌套对象，而 argv 是唯一会进日志和进程列表的地方。

**内置规则可以关，但不能删。** 删掉一个 override 的效果是内置规则回来，这跟「我不想再拦这个动作」
不是一回事，所以 `rule remove` 对内置规则直接报错并告诉你该用什么。关掉的写法是**只给 id 加
`enabled: false`**——不重述 matcher，否则那份副本会跟真的那条漂移，然后某天悄悄不拦了。合并因此分两种
语义：override 带 matcher 就整条替换，不带就当成对同 id 内置规则的 patch（只改 enabled 和 label）。

关掉的规则在匹配前就被过滤掉，不会被求值——所以一条关着的 patch 不可能报出 matcher 错误、把整个工具
的调用变成 fail closed。

**安装位置记在 `guard.tools.<tool>.bin`。** 这不是可选的：`dws` 不在 PATH 上，不记下来
`guard status` 和 `atm doctor` 在进程退出后就再也找不到它，clobber/shadow 检测对它等于没跑——而它正是
最该检查的那一个。写配置一律走 raw map 合并，所以这个 build 不认识的字段不会被踩掉。

规则在 `~/.atm/config.json` 的 `guard` 段，无配置即生效。匹配**只看命令开头那一串非 flag token**，
不看整条命令行：否则 `--text` 里出现 `ata::message-...` 这样的正文就能给自己选规则。同理
`argv_pattern` 逐 token 锚定匹配，绝不对拼接后的命令行做正则。

### 决定它的两个地方

通知自带「批准并发送 / 拒绝」两个按钮，按下去就是决定本身——不打开窗口，也不路由到任何地方。这需要
注册一个 `UNNotificationCategory`：在此之前 App 从来没注册过任何 category，那些设了
`categoryIdentifier` 的通知其实一个按钮都长不出来。「批准」按钮是 foreground 的，因为批准会真的执行
命令，「批准了但发送失败」必须有个地方能报出来。

漏掉通知时走快速面板（状态栏左键、全局热键、状态菜单都能开），顶部列出待授权的几条，带正文和同样两个
按钮。**没有为它新开桌面工作区**：这是「扫一眼 + 做个动作」的场合，不是要长时间待着的地方。

通知按请求 id 用稳定标识符，所以同一条请求只会替换、不会堆叠；请求被决定（在这儿、在终端里都算）或
过期后主动撤回——一条还挂着活按钮的过期通知，比没发过更糟，按下去只会得到一个莫名其妙的失败。首次
加载不补发历史积压的通知，否则开一次 App 就是一屏横幅。

socket 上走的是判别式联合（`type` 字段），不是给 agent 事件加第六种 kind：授权请求跟会话无关，而新增
kind 会被所有消费 agent 事件的地方读到——给会话记上「会上报回合状态」、并进 cwd、强制刷新一次状态，
没有一条对它成立。没有 `type` 的行仍然是 agent 事件，所以已经装好的 hook 一个字节都不用改。

### 判断上的几个取舍

- **匹配不确定就放行，ATM 自己坏了就拦住。** 漏拦一次等于回到没有闸门之前；误拦一个读命令会让这
  功能被卸掉，之后什么都拦不住。所以匹配器偏向假阴性。但**已经匹配上之后**打不开数据库，则拒绝执行
  （exit 70）——静默发出去正是要防的那件事。快路径根本不碰数据库，所以 ATM 出问题不会影响读命令。
- **等待预算短（默认 25s），请求寿命长（默认 30 分钟）。** 等待只负责接住「人正好在桌前点了一下」。
  等得久反而更糟：Agent 对 shell 命令有自己的超时，一旦被它 SIGKILL，写给模型的那段 stderr 就送不到
  了——而那段话是整个设计里最有用的产物。
- **执行权归属看时钟，不看 PID。** 请求寿命长到足以让操作系统复用 PID，`kill(pid,0)` 在这里不可用。
  行上记的是 `gate_deadline`：等待中的进程在 deadline 前拥有执行权，之后归抢到 claim 的人；干净退出
  时立刻交还，常见情况不用等满。
- **`running` 对自动化是终态。** 闸门在 `running` 和 `done` 之间死掉，就是「不知道消息发出去没有」
  ——这个信息不存在，任何锁都补不回来。绝不扫描重试（重试会重复发消息）。`atm guard show` 会直说
  结果未知，让你自己去目标确认。
- **内容哈希只做 dedup key，不做主键。** 真正的不变量是「同一条命令最多一个 pending」，靠
  `UNIQUE(dedup_key) WHERE status='pending'` 保证，所以 Agent 重试是挂到同一行、不会再弹一次。用哈希
  当主键会毁掉审计：先拒后批就只剩一行，而「你拒过」正是这张表唯一要留住的东西。

### 退出码

复用 sysexits，避开子进程自己常见的 1/2。批准后内联执行时**逐字透传子进程的退出码**。

| 码 | 含义 |
|---|---|
| 子进程的码 | 已执行，原样透传 |
| 70 | ATM 自身故障，已阻止 |
| 75 | 等待超时，请求仍待批 |
| 77 | 已拒绝 |

### 批准时 ATM 会执行什么

批准后由 ATM 自己跑那条命令——等你决定的时候，提出请求的 Agent 通常已经被告知不要重试并往下走了，
要求它重跑就意味着永远没有下文。这让 `approvals` 成了一张「存起来的可执行命令」表，因此收窄了三处：

- 只执行**被 shim 移开过**的二进制，且该工具此刻确实处于已启用状态
- **不重放 Agent 的环境变量**（那等于往 SQLite 存密钥）；命令在 ATM 自己的环境里、按记录的 cwd 执行，
  所以「批准了但认证失败」是可能的结果，退出码和输出都记在行上
- stdin 带了内容的请求拒绝延迟执行（stdin 无法重放）。判断刻意收窄成「FIFO 或普通文件且真有字节」，
  而不是「stdin 不是终端」——实测 Agent 给子进程的 stdin 是 **socket**，`/dev/null` 是字符设备，按
  「不是终端」判断的话几乎每次调用都会命中，延迟执行这条路就永久废了

过期不靠扫描也不靠 daemon：读命令是 `query_only` 的，写不了。列表用 SQL 现算有效状态，
`approve` 发现超时就在同一个写事务里翻成 `expired` 并拒绝。任何清理路径都**不会执行任何东西**——
执行只可能来自你的一次明确操作。

### 拦不到什么

- **MCP 工具。** 闸门只看命令执行。通过 MCP 完成的外发动作不经过 `execve`，ATM 没有任何介入点。
  装了闸门之后 `atm doctor` 会读三个 Agent 已有的 `mcpServers` 配置，非空就提醒——不是因为 MCP 有
  问题，而是因为装了闸门的人会停止盯着外发，这时候一个看不见的通道比没装闸门更危险。
- **铁了心要绕的 Agent。** 移开的真身就在旁边，以同一 uid 运行的进程，文件系统层面挡不住。这是防
  「不假思索」的护栏，不是防对手。真正起作用的硬化是拒绝话术里那句「不要换用其他命令或工具绕过」
  ——它在决策实际发生的那一层（模型的下一个 token）对所有 Agent 同时生效。

## 存储与文件

### 权限

`~/.atm` 建为 `0700`，`config.json` 与 `credentials.json` 写为 `0600`：这个目录里既有会话正文也有
一把 API Key，默认不该对同机其他用户可读，旧安装的宽权限在下一次写配置时被收紧。
`credentials.json` 权限比 `0600` 宽时 ATM 拒绝读取并提示 `chmod`，而不是照用一把可能已被别人
看过的 Key。

### SQLite

结构化观测数据、工作状态和连接器审计都存储在 `~/.atm/atm.db`（SQLite + WAL）。Todo、tag、依赖、
link、图片、Session Binding 和 Comment 使用规范化表，状态与优先级枚举、日期格式由 CHECK 约束保证，
Comment 和 Binding 通过外键随 Todo 级联删除。写入统一走一个事务：先取写锁再读快照，因此不需要
乐观版本号；生命周期变更与 Binding 关闭/创建在同一次提交里落库。

Todo 图片采用「SQLite 关系 + 受管本地文件」：`todo_images` 保存顺序、受管文件名、原文件名、媒体类型和
字节数，文件位于 `~/.atm/todos/assets/<todo-id>/`。路径在读取时由 ATM 数据目录与受管文件名重新计算，
不把机器上的绝对路径固化进数据库。导入先验证 PNG/JPEG/WebP/GIF/HEIC 的扩展名与文件头、10 MB 单文件
上限和 10 张总数，再以 `0600` 权限原子复制；数据库事务失败会回滚刚复制的目录。Archive/Trash 只移动
Todo 可见性，资源保持不动；永久删除由外键清除元数据，并在事务成功后删除对应资源目录。

`approvals` 是这个库里唯一一张**内容之后会被交给 exec 的表**（[外发动作闸门](#外发动作闸门)），
所以 `atm guard approve` 只肯执行被 shim 移开过的二进制。它也存着待发消息的正文和接收方——这是这个
功能的全部意义所在，代价是这些内容会随 `atm backup` 进备份包，见 [PRIVACY](../../PRIVACY.md)。

### 日志

失败会落盘到 `~/.atm/logs/`：CLI 写 `cli.log`，菜单栏 App 写 `app.log`，一行一个 JSON 事件，
单文件封顶 5 MB 并保留一个轮转。**只记失败和进程启停**，不记会话正文、Todo/记忆/知识内容、凭据，
也不记命令参数（`atm todo add "<标题>"` 的标题本身就是内容），所以日志里是 `atm todo add` 而不是
完整命令行。App 无法在自己崩溃时写日志，因此用一个「上次是否正常退出」的标记来区分崩溃与正常退出，
并在日志里指向 macOS 的 crash report 目录而不是把报告内容抄进来。`atm diagnose --bundle` 会带上
两个日志的最后 200 行 —— 这是「每天失败一次、其余时间正常」这类间歇故障唯一能被看见的地方。

### backup / restore

`atm backup` 归档这个库真正无处重建的部分：Todo（包括 `todos/assets/` 下的图片）、共享记忆、中央知识、连接器收集账本和 review 游标。
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
`knowledge delete` 永久移出 ATM，导入源文件保留。文档与 collection 的永久删除都由 Knowledge Service
强制要求显式确认：CLI 交互确认或 `--yes`，App 只能在确认框完成后发送 `confirmed: true`。

### Memory 与 Artifact

共享记忆以 append-only `~/.atm/memory/events.jsonl` 保存；artifact 原子写入
`~/.atm/artifacts/`。ATM 不在运行时探测其他产品的数据目录，历史数据应通过显式 importer 转换。
Knowledge、Memory 和 Artifact 只通过 ATM CLI 访问。

## App 与 CLI 的协议

ATM 是模块化单体，不把 `internal/cmd` 当业务层。依赖方向固定为：

```text
Cobra CLI ───────┐
App typed _ipc ──┼──> 分领域 Application Service ──> domain / store ──> SQLite、文件与副作用 port
Run controller ──┘
```

`internal/cmd` 只解析 Cobra 参数、做终端确认并渲染；`internal/ipc` 只解码 typed request、注入
`human@ipc` 调用身份并编码 envelope。校验、事务、幂等和跨步骤工作流放在 `config.Service`、
`aiday.Service`、`work.Service` 等领域 service。任何非 cmd 包都禁止反向导入 `internal/cmd`；IPC 也禁止
依赖 Cobra，这些方向由 `internal/architecture` 的 AST 测试锁住。

这里的 `human@ipc` 是 adapter 来源标签，不是经过认证的用户身份。`_ipc` 为了排障可在终端重放，任何本机
Agent 也能直接启动它，因此当前只承载读、配置和不依赖人类授权的工作流。Guard approve/deny 仍走会识别
Agent 环境并由 `guard.Service` 再做 human-only 校验的 CLI；在 transport 能证明 App 身份前不得注册成
`_ipc` verb。CLI 的环境识别同样只是 best-effort：调用方能删掉 ambient environment，所以它不能单独
构成抗恶意 Agent 的认证。要把 human-only 从产品策略提升为安全边界，需要 user-presence 或可验证的可信
App channel。

App 专用调用使用隐藏的 `atm _ipc <method>`。请求参数从 JSON stdin 读取，成功和失败都返回同一个
envelope：`envelope_version`、`protocol_version`、`request_id`、`verb`，以及互斥的 `data` 或
`error`。领域错误使用稳定的 `invalid_argument`、`not_found`、`conflict`、`forbidden`、`busy`、
`unavailable`、`internal`；App 即使看到非零退出码也先解析 stdout 中的 error envelope，只有无法解析时
才把它当 transport failure。Method 按数据或原子工作流聚合，不按每条 CLI 或每个 Swift screen 镜像。

迁移按领域进行。Config 的设置快照/原子保存、AI Day 的快照/反馈/权限/导出/删除、Dashboard 聚合快照、
Knowledge 的 catalog/query/get/governance 读模型与文档/collection/feedback 写工作流、Collector 的
快照/采集/历史、来源管理与记录动作、Session 的 list/search/show/timeline 读模型、Todo 的
list/show/doc 读模型、create/update 元数据工作流与 refine 整理工作流、Guard 的只读待批
列表，以及 Doctor 自检与 Quota 快照已走 typed IPC。Collector 的 App
契约按 use case 聚合成 typed request；不会透传 argv，也没有 `action + map` 万能入口。来源/记录删除和
记录撤销仍由 `collector.Service` 强制要求 `confirmed=true`，但这个字段只防误操作，不能把可重放的
`human@ipc` 变成身份认证。Knowledge、Collector、Session 和 Todo 的相应公开 CLI 仍作为 Agent、人工恢复或诊断入口，
并与 IPC 共用各自的 service；App 不再拼这些已迁读取路径的 argv。Guard 决策因上述认证边界继续走 CLI。
尚未迁移的普通 argv 仍列在
`app/macos/atm-cli-contract.txt`，Go 契约测试会反扫 Swift 字面调用，避免改名后只在运行时坏掉。普通命令
是否保留由 Agent、人工恢复和后台消费者决定，和 App 是否已迁 IPC 是两个问题。

Quota、Doctor、Diagnose、Sync 和 Report 各有独立 Application Service。这五个域此前没有 service，
约 1600 行编排住在 Cobra 里：`quota.Service` 拥有三个 agent 的日志读取顺序、趋势查询的降级、provider
子进程与占位卡片缓存；`doctor.Service` 拥有来源/coverage/定价/collection/todo 依赖的全部判定，闸门
findings 由 `guard.Service.Diagnose` 提供并在查不动时降级为无发现；`sync.Service` 拥有扫描范围选择、
搭车的额度采样，以及「只读状态不许顺手建库」这条；`report.Service` 拥有日期解析与「哪些 session 不
值得列」的取舍；`diagnose.Service` 拥有 bundle 的采集范围、`$HOME` 重写、拒绝覆盖与 0600。
`doctor --json` 与 `_ipc doctor.check`、`quota --json` 与 `_ipc quota.snapshot` 各自是同一个结构的
同一份序列化，CLI 与 App 不会在键名上分叉。

AI Day 的日常快照、单日读取、反馈、来源/隐私设置和删除只通过 typed IPC 暴露给 App；不再为这些
App 工作流保留同形 Cobra 叶子。公开 `day` 命令只剩 `rebuild`、`badge`、`sources list` 和 `export`，分别
服务历史投影修复、判定证据诊断、来源状态诊断和无原文数据导出，四者都直接调用 `aiday.Service`。

macOS App 的主周期刷新调用 `_ipc dashboard.snapshot`，从 JSON stdin 传 section 与可选 Session ID；
普通 `dashboard` Cobra 命令已删除。聚合、section 校验、Todo/绑定/索引/统计并发读取都由
`dashboard.Service` 负责，CLI transport 不再实现第二份规则。实时进程与 transcript 探测目前仍由 cmd
侧实现并通过只读 port 注入，这是待独立成 live-status read service 的明确过渡边界，不属于 Dashboard
领域逻辑。Dashboard payload 继续遵守版本化契约
[`docs/contracts/dashboard-v1.schema.json`](contracts/dashboard-v1.schema.json)，刷新时并发调用一次
`_ipc quota.snapshot`。额度来自各 Agent 自己的日志而不是会话索引（Codex 会话 `rate_limits`、Grok
`~/.grok/logs/unified.jsonl` 里的 billing credits 与账期刷新时间）。私有或第三方额度源可通过
[`quota_providers` 版本化命令协议](quota-provider-protocol.md)提供通用多指标卡片，无需链接进 ATM 或
暴露服务凭据；任一额度源读取失败只影响自己的卡片。
