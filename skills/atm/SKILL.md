---
name: atm
description: 使用 ATM CLI 查询和管理跨 Agent 的注意力、会话、TODO、共享记忆、中心化知识、artifact、统计与配额，并在 ATM 任务与 Git 改动之间执行代码评审交接。用户提到 ATM、当前焦点、最近会话、历史对话、某个 session、跨项目任务、todo 状态、等待条件、工作恢复、token 用量、agent 状态、共享记忆、知识检索，要求基于 ATM Todo 审核/复核代码改动，或需要沉淀应用名/ID、发布流水线、环境、部署与运维信息时都应使用。特别地：当消息中出现形如 `t65`、`#t65`、`#65` 的 ATM Todo ID，或 `s<数字>` 会话 ID 的简写引用时——哪怕只是“更新下 #t65 ”“看下 t69”“t91 做完了”这类几乎没有其他语义线索的极短指令——都应识别为 ATM 操作并使用此 skill。即使用户没有明确说“运行 atm”，只要任务需要从多个 AI Agent 或多个项目恢复真实工作状态，也应优先使用此 skill。
---

# ATM

ATM 是跨项目、跨 Agent 工作状态的事实源。使用它恢复“现在在做什么、为什么停下、下一步是什么”，并把本轮产生的真实状态变化写回 ATM。

需要本机已安装 `atm` CLI。查询默认读取本地索引且不写数据库；`--sync` 是需要最新数据时的显式刷新手段。

详细命令按需读取 [references/commands.md](references/commands.md)。不确定参数时先运行对应子命令的 `--help`，不要凭记忆猜 flag。

## 基本原则

- 会话、TODO、共享记忆和知识优先从 ATM 查询，不默认扫描 Codex、Claude、Pi、Copilot 或 Qoder 的原始私有目录。
- 先读后写。查询请求不授权修改 TODO、记忆、知识或 artifact。
- ATM TODO 是跨项目工作状态的唯一事实源。Agent 启动时优先使用已注入的 `ATM current` 或最多 3 条 `ATM candidates`；不要为了启动恢复把完整 `atm now --json` 注入上下文。
- 创建 TODO 前先用 `atm todo match "<具体目标>" --dedup` 查重，避免同一目标产生重复任务。它跨项目搜索并要求真实的 query 相关性，会明确回答「无匹配，可以新建」；`--prompt` 是启动注入用的，总是返回若干候选，不能用来做查重判断。
- 小改动如果能在当前回合完成，不创建 TODO。只有跨会话主线、明确等待项、长期维护项或用户明确要求跟踪时才创建。
- 使用具体仓库名作为 project，不使用 `work`、`mox` 等容器目录名。
- 原始 JSON 只是证据。默认向用户输出结论、时间、项目、状态和下一动作，不倾倒大段原始会话或无关系统提示。

## 1. 启动匹配与恢复当前工作

启动注入只提供当前会话绑定或当前仓库的少量候选。读完用户请求后：

1. 若 `ATM current` 与请求一致，继续使用当前绑定；请求已改变则先 `atm session unbind --reason scope-changed`。
2. 若某条 `ATM candidates` 与具体目标一致，运行 `atm session bind <id>`；绑定会同时把 open todo 标成 in_progress。
3. 候选不够时运行 `atm todo match "<具体目标>" --prompt` 做一次轻量匹配。
4. 没有匹配项时先保持未绑定；只有跨会话主线、外部等待项或用户明确要求跟踪时才新建 TODO。

需要人工查看跨项目主线、等待、评审、阻塞、到期或维护全局面板时，才运行：

```bash
atm now --json
```

按工作/个人 lane 查询时使用 `--lane`。需要进一步确认任务详情，再使用 `atm todo show`、`atm todo list` 或对应 session 查询。`atm now` 是全局仪表盘，不是常规 SessionStart 上下文。

TODO 只使用一套生命周期状态，不再维护独立的 attention 状态：

- `open`：待开始，队列顺序由优先级和创建时间推导。
- `in_progress`：工作中；当前会话的具体焦点由 session 绑定表达。
- `waiting`：工作已暂停，存在明确外部唤醒条件或复查日期。
- `review`：等待人审阅、验收或决策。
- `blocked`：当前无法继续，且没有可执行替代路径。
- `done/dropped`：已完成或已放弃。

`maintenance` 是范围标签，不是状态；使用 `atm todo maintain` 设置。

实时活动、Session binding 和 Todo 生命周期是三个正交事实：

- `session status.sessions[].activity_state` 只表示近期是否观察到 Agent 会话活动；
- `binding_state` 只表示显式 Session→Todo binding 是否有效；`bound` 要求目标 Todo 当前为 `in_progress`；
- `todo.status` 表示工作状态；`in_progress` 可以暂时没有活跃 Session，也不能按项目名推断某个 Session 已绑定。

不要把“同项目”“存在 in_progress Todo”或“检测到 Agent 进程”写成显式 binding。`session status --json`
会把未观察到实时活动的 binding 放在顶层 `bindings`，并以 `todo_missing` 或
`todo_not_in_progress` 暴露失效关系；`session current --json` 同样返回 `state`，不要只看旧的
`bound` 布尔值。

## 2. 查询会话

按以下顺序缩小范围：

1. `atm session list --days <N> --json` 找最近会话；
2. `atm session search <keyword> --json` 按主题找候选；
3. `atm session show <session-id> --json` 读取完整问答；
4. 需要事件顺序时使用 `atm session timeline <session-id> --json`；
5. 需要当前 Agent 活动时使用 `atm session status --json`。

需要从多个历史会话抽取、筛选并沉淀共享记忆时，使用 `atm-memory-curator` skill。普通会话查询和单条明确事实的 memory CRUD 仍按本技能执行。

优先读取真正相关的少量 session。搜索词过宽时，用 `--project`、`--days/--since`、`--role` 缩小范围，用 `--limit` 和 `--snippet` 控制返回量，避免被仓库说明、系统提示或重复上下文淹没。

`session search` 默认只返回最新 50 条命中、每条 400 字符片段。返回的是一个信封：`total` 是过滤后的命中总数，`returned` 是本页条数，`truncated` 说明是否被 `--limit` 截断。不要把 `matches` 的长度当成命中总数——默认查询经常是 1000+ 里的前 50 条。需要完整问答时用 `session show`，并用 `--last`、`--turns` 或 `--max-chars` 限制单会话的返回量（它同样返回 `total_turns`、`returned_turns` 和 `truncated`）。

### 索引新鲜度与 `--sync`

默认先执行不带 `--sync` 的只读查询。macOS Menubar 进程运行时会在启动时同步，并按 5 分钟节奏后台更新会话索引；不要为每次查询重复扫描 session 源、写 SQLite 或触发沙箱权限申请。

无法判断索引是否新鲜、Menubar 报同步异常，或需要向用户说明证据边界时，先运行只读的：

```bash
atm sync status --json
```

它报告索引是否存在、最近尝试与成功时间、数据年龄、最近错误和已索引会话数，不会自行同步或迁移数据库。`status=fresh` 表示最近成功同步仍在新鲜窗口内；`stale/failed/missing/never` 再结合用户是否要求最新数据决定是否显式同步。

只在以下情况使用 `--sync`：

- 用户明确要求最新、实时或包含刚产生的会话；
- 本地索引不存在，ATM 明确提示先运行 `atm sync`；
- 有充分理由确认目标会话应已存在，但只读查询未命中；
- Menubar/后台同步未运行、失败，或当前环境不提供它。

`--sync` 会在读取前同步 session 源并写 ATM 本地索引。例如需要强制刷新后搜索：

```bash
atm session search <keyword> --json --sync
```

如果在 Codex 等受限沙箱中出现 `attempt to write a readonly database`、缓存不可写或同类权限错误：

1. 保留原始错误，确认 ATM 命令本身可用；
2. 申请最小范围的沙箱外权限；
3. 重跑完全相同的 ATM 命令；
4. 权限理由只说明同步本地 session 索引并读取结果；
5. 不借机修改代码、TODO 或外部状态。

只有无法获得沙箱外权限时，才降级到不带 `--sync` 的已有索引，并明确说明可能漏掉最新会话。不要因此绕过 ATM 去扫描各 Agent 的原始 session 文件，也不要把每次查询都升级为写操作。

## 3. 管理 TODO 生命周期

### 开始工作

先搜索当前仓库和具体目标的现有 TODO。Agent 会话真正接手时优先绑定：

```bash
atm session bind <id>
```

无法取得会话 ID 的人工终端才使用 `atm todo start <id>`。绑定关系会保留历史，可用 `atm session current` 或 `atm todo show <id>` 查看。

用户要把任务交给另一个 Agent 时，用 `atm todo prompt <id> --copy` 复制一行指针交给用户粘贴；
ATM 不代为启动任何会话。收到这样一行指针时，按它列出的命令读 `atm todo doc <id>` 再 `atm session bind <id>`。

任务依赖另一个 ATM todo 时，使用 `atm todo depend add <id> <dependency-id>` 保存结构化关系，
不要只把 `tNN` 写进自由文本 wake condition。依赖全部完成后 ATM 会自动将 waiting 任务恢复为 open；
依赖就绪只表示工作可以开始，不表示实现已提交 review。完成实现后用 `atm todo submit` 显式提交确认；
外部流水线、MR 或人工条件满足时使用 `atm todo wake`，异常恢复时可运行只影响依赖状态的
`atm todo reconcile`。

重复执行同一 `depend add` 是幂等的；派生的 `waiting for todos: ...` 会按完整依赖集合刷新。
Todo 离开 `in_progress` 时 ATM 会先关闭 binding 再保存新状态。需要审计迁移时读取
`atm todo show <id> --json` 的 `bindings[].unbound_at/reason`，不要用当前状态倒推历史。

### 记录里程碑

`atm todo log` 只记录已完成里程碑或进入外部等待，不记录调查过程、方案推演和操作流水。完成有意义的阶段后：

```bash
atm todo log "结果：<交付变化>；证据：<验证边界>；下一步：<唯一动作>"
```

会话已绑定时，`log/show/doc/lint/done/wait/drop` 均可省略 ID；显式 ID 仍兼容。

进展日志必须遵守：

- 每个阶段最多一条完成动态；中间准备状态不写，除非工作暂停在可观察的外部条件。
- 单条不超过 400 个 Unicode 字符，只写一个段落。源码调查、架构映射、备选方案和列表写到
  `atm todo log <id> "<detail>" --section 分析`，不要塞进 `进展`。
- 状态先结构化、后留痕：开始用 `start`，完成用 `done`，等待用 `wait`，其他状态用 `edit --status`，维护标签用 `maintain`，依赖用
  `depend`。日志不能代替这些状态命令，也不能代替 description 中的真实清单更新。
- 日志里提到的 `tNN` 必须已经创建且可由 `atm todo show <id>` 查到；不得先在自由文本中声称拆出了不存在的子任务。
- 写完或接手历史任务时可运行 `atm todo lint <id>`，检查超长/多段动态、未知 todo 引用、重复阶段日志和 Markdown 元数据漂移。

### 完成、等待与工作状态变化

实现已完成、需要人或下游流程确认时：

```bash
atm todo submit --reason "<结果与证据>"
```

`submit` 只把 `in_progress` 任务提交到 `review`，不会标记 `done`。Agent 后台 run 成功退出时也只能
走这条提交路径；模型输出或进程退出码本身不是完成事实。

全部完成且没有剩余必需工作时：

```bash
atm todo done --reason "<最终结果>"
```

暂停在外部条件时：

```bash
atm todo wait --wake "<可观察的唤醒条件>"
```

`done`、`drop` 和 `wait` 会自动解除关联会话，避免下次启动沿用失效任务。

主线、优先级、lane、状态或维护范围发生变化时，使用 `start`、`edit --status` 或 `maintain`。不要只在回复里描述状态变化而不更新 ATM。

永久删除是破坏性操作。除非用户明确要求删除错误数据，否则优先使用 `done` 或 `drop` 保留历史。

## 4. 在 ATM 任务中执行代码 Review

仅当代码审核需要对照 ATM Todo、恢复其他 Agent 的任务上下文或把审核结论写回 ATM 时使用本节。普通、一次性的本地代码 review 如果没有匹配 Todo，保持未绑定且不要为此创建 Todo。

### 事实边界

- ATM Todo 是工作状态的唯一事实源：保存目标、约束、验收条件、生命周期、外部等待和下一动作。
- Git 工作区是实现状态的唯一事实源：以实际 tracked、staged 和 untracked 文件为准，不用 Todo 中的文件清单替代检查。
- 测试与检查结果是验证证据，不等于需求事实；没有真实运行过的命令不得写成“已通过”。
- Session、run、进展和分析记录是追溯证据，不自动覆盖 Todo 的当前状态。

代码可能实现错误，Todo 也可能已经过时。Code Review 的职责是发现两者不一致并给出证据，而不是把其中任意一方声明为所有信息的唯一事实。

### 授权模式

- `review`：用户只要求 review、审查、检查或评估时，只读检查并报告问题，不修改代码、测试或配置；不要机械改变 Todo 状态，但真实生命周期变化仍按 ATM 规则写回。
- `review-fix`：用户明确要求“发现问题直接修”“按审核结果修复”或同等授权时，才修改代码；只修复本轮审核确认的问题，不扩大范围或顺手重构。
- 问题涉及产品取舍、兼容性、数据迁移、安全边界或其他无法安全决定的事项时，不擅自修复；写明影响和所需决策。只有任务真实暂停在外部条件时才进入 `waiting`。

Todo 的 `review` 状态表示等待人审阅、验收或决策，不表示代码已经完成 Code Review。不要仅因执行了一次代码检查就机械改变 Todo 状态。

### Review 流程

1. 确认当前会话绑定的是本次代码任务；读取 Todo 的最新目标、约束、验收条件、未决事项和必要的少量历史。
2. 优先运行 `atm todo context [id] --json` 取得即时、只读上下文；单一活跃 worktree 会自动选择，多个活跃 worktree 时必须用 `--cwd` 明确目标。该命令只汇总 Todo、Session 绑定、Git revision、staged/unstaged/untracked 文件和历史里程碑，不持久化 handoff、不输出完整 diff、不运行测试、不修改状态。`review-context` 只是兼容别名。
3. 继续检查 `git status --short`、`git diff` 和 `git diff --cached`，并按 context/status 读取相关 untracked 文件。上下文快照不能替代实际 diff；用户指定 commit、branch、MR/PR 或 base 时，以指定比较范围为准。
4. 逐项对照需求检查本轮所有相关改动，覆盖行为正确性、回归风险、错误处理、安全与隐私边界、兼容性及测试缺口；不要只看风格，也不要只抽查部分 diff。
5. 运行与风险相称的测试或静态检查，并准确记录命令、结果和未覆盖边界。`context.verification.status=not_run` 只表示该命令本身没有运行测试；历史里程碑中的测试结论仍是未独立核验的追溯证据。
6. 按严重级别输出 findings，优先给出文件与行号、触发条件、影响和建议。没有发现问题时明确说明，并列出尚未验证的风险或测试缺口。
7. 只有审核导致真实任务状态变化时才写回 ATM：里程碑结论写一条 `todo log`，详细 findings 写入 `--section 分析`；等待决策用 `wait`，等待验收用 `review`，全部目标真正完成才用 `done`。不要复制整段 diff 或用日志代替结构化状态。

## 5. 记忆、知识与 artifact

根据意图选择数据域：

- `memory`：稳定、可复用的事实或偏好；先 recall，再决定是否 remember/supersede/forget。
- `knowledge`：可检索文档；先 catalog/search，再 get，避免无目标读取整个知识库。
- `artifact`：版本化产物，例如报告、方案和最终 Markdown。

Knowledge collection 是 `~/.atm/knowledge/<id>/` 目录，不是文档 frontmatter 字段。集合级新建、
编辑 manifest、重命名和删除使用 `atm knowledge collection`；删除非空集合必须明确 `--force`
或 `--move-to`，不要直接操作目录绕过冲突检查。

读取请求不授权写入。新增、替换或遗忘长期记忆，导入知识，保存 artifact，都应符合用户当前目标，并保留来源、状态和可撤销路径。不要把临时推测写成长期事实。

从会话抽取出的 memory 必须用 `--source "session:<id>#turn:<n>"` 保存来源。预览候选时不要写 memory，也不要调用 `atm session review` 推进整理游标。

Agent 在真实任务中使用中央知识时，应把当前 ATM session ID 传给 `knowledge search --session`；形成答案后，
对实际使用的文档调用 `knowledge feedback` 标记 `adopted`、`corrected` 或 `rejected`。纠正或拒绝时用
`--note` 写清原因。不要为没有读过或没有进入答案的搜索结果制造正向反馈。`knowledge quality` 用于查看
聚合质量，`knowledge audit` 用于只读巡检重复、陈旧、源文件漂移和低质量条目；巡检不会自动归档。

### 应用知识卡

应用研发、发布、部署、运维或 CLI 交付中，一旦已核验到可复用的应用基础信息，就把它沉淀为 ATM 中心化知识；用户明确要求“记录应用信息”“存知识库”时更应立即执行。这样后续 Agent 不必反复猜测应用 ID、环境与发布入口。

1. 先用 `atm knowledge catalog` 和按应用名/仓库名的 `atm knowledge search` 查重。
2. 所有应用知识卡统一写入 `应用管理` collection，不再按应用建专属 collection；用精确的应用名/仓库名作为标题前缀并通过 `--project` 区分具体应用。已存在同应用知识卡时原地更新。稳定项目知识默认不写入 `inbox`。
3. 只记录已经由平台、仓库配置或已完成操作核验过的内容，并在正文注明核验时间和来源链接/命令。常见字段包括：
   - 应用名、应用 ID、代码仓库、默认主干和关联包/CLI；
   - 环境名称与 ID；发布流水线的名称、ID、入口 URL 与适用范围；
   - 稳定的发布/回滚入口模式，以及重复出现的人工门禁或诊断步骤；
   - 长期兼容性约束、支持的平台/架构和已知限制。
4. 不记录密钥、令牌、个人凭据、临时 URL、未经验证的推测，或仅对一次操作有意义的噪声。单次 CR、commit、功能/release 分支、发布实例、Run ID、瞬时流水线状态和当时的版本号属于发布履历，默认留在 CR、TODO 或复盘，不写入应用知识卡；只有沉淀为长期运维规则时才提炼。
5. 信息变化时原地更新同一知识卡并更新时间、状态和来源；不要反复新增同标题文档。完成后用 `atm knowledge get` 或检索验证该卡可读取。

默认知识卡应说明：它服务哪个应用/仓库、信息的核验时间、当前状态和下一次应何时复查。用户只要求查询或分析时仍保持只读，不因看到了应用信息而擅自写入。

## 6. 统计与诊断

- Token、模型、请求和 session 用量：`atm stats`。
- Agent 配额：`atm quota`。
- 数据源与请求覆盖：`atm doctor`。
- 日报草稿：`atm report`；更完整的日报/周报使用 `work-report` skill。

## 7. 最终核对

在最终回复前检查：

- 本轮是否真的开始了已有 TODO，需要 `start`；
- 是否完成了有意义里程碑，需要 `log`；
- 是否已提交确认、完成、进入等待或改变工作状态，需要 `submit`、`done`、`wait`、`start`、`edit --status` 或 `maintain`；
- 是否误改了无关 TODO；
- 代码 review 是否区分了 `review` 与 `review-fix`，并检查 tracked、staged、untracked 改动；
- 是否把 `context` 当作即时上下文快照而非持久 handoff、完整 diff 或测试结果；
- 是否把 Git 实现状态、测试证据和 ATM 工作状态错误地混成同一个事实源；
- 是否把调查细节误写进 `进展`，或用动态代替了 description/status/dependency 的结构化更新；
- 新写的进展是否为单段、400 字以内，引用的 `tNN` 是否真实存在；
- 是否在 Menubar 已维护索引且不要求实时性的情况下滥用了 `--sync`；
- 是否把权限错误误报成 ATM 数据缺失；
- 是否清楚区分已有索引与 `--sync` 后的最新索引。

最终输出应包含最重要的 ATM 结论、证据范围和任何状态写回结果。没有发生状态变化时，不需要为了“留痕”制造无意义更新。
