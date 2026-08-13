# ATM 命令路由

参数以本机 `atm <command> --help` 为准。以下只列常用入口和副作用边界。

## 当前工作与会话

```bash
atm now --json
atm session status --json
atm session list --days 7 --json
atm session list --days 7 --project <repo> --json
atm session list --since <RFC3339-or-date> --review pending --json
atm session search <keyword> --json
atm session search <keyword> --limit 20 --project <repo> --role user --days 7 --json
atm session search <keyword> --snippet 200 --json
atm session search <keyword> --json --sync
atm session show <session-id> --json
atm session show <session-id> --last 5 --json
atm session show <session-id> --turns 1-10 --max-chars 4000 --json
atm session show <session-id> --thinking --json
atm session timeline <session-id> --json
atm session export --days 7 --format json
atm session review <session-id> --outcome none|memory|knowledge|mixed --note "<result>" --json
```

`--agent` 可过滤 `claude`、`codex`、`pi`、`copilot`、`qoder`、`qodercli`、`qoderwork`。
`session status` 的 `activity_state`、`binding_state` 和 `todo.status` 分属实时观测、显式关系和
工作生命周期。顶层 `bindings` 包含没有实时活动的有效或异常 binding；不要用项目名或
`in_progress` Todo 猜测 Session 绑定。`session current --json` 的 `state` 可能是
`unbound/bound/todo_missing/todo_not_in_progress`。

会话查询默认不带 `--sync`，读取 Menubar/后台同步维护的本地索引；只有明确需要最新数据、索引缺失、预期会话未命中或后台同步不可用时，才把 `--sync` 作为显式刷新覆盖。

不确定索引是否可信时先只读检查：

```bash
atm sync status --json
atm sync status --agent codex --json
```

结果中的 `sync.status` 为 `missing/never/syncing/fresh/stale/failed`；`last_success_at` 与 `age_seconds` 表示当前索引证据的新鲜度，`last_error` 保留最近失败原因。该命令不触发同步；需要刷新时再运行 `atm sync` 或给目标查询加 `--sync`。

## TODO 读取

```bash
atm todo match --prompt                         # compact local candidates for startup
atm todo match "<goal>" --prompt               # local/semantic candidates
atm todo match "<goal>" --dedup                 # 查重：是否已有 todo 覆盖该目标
atm todo match "<goal>" --dedup --json          # duplicate 布尔 + query_score
atm session current                             # current session binding
atm todo list --json
atm todo list --status all --json
atm todo list --project <repo> --json
atm todo list --status waiting --json
atm todo list --creator collect --json        # who filed it: me, collect, or an agent name
atm todo show <id> --json
# `bindings` 历史中的 unbound_at/reason 是 Todo 状态迁移的结构化审计证据
atm todo context [id] --json                  # live, read-only Todo/session/Git context; does not run tests
atm todo context <id> --cwd <worktree>        # explicitly inspect another worktree
atm todo review-context [id] --json           # deprecated compatibility alias for todo context
atm todo prompt <id>              # the pointer line a human pastes into a fresh agent session
atm todo doc <id>
atm todo lint <id>                # audit progress verbosity, references, and markdown consistency
```

## TODO 写入

```bash
atm todo submit [id] --reason "<summary/evidence>" # in_progress -> review; never marks done
atm todo add "<title>" --project <repo> --priority P1 --status open --desc "<description>"
atm todo add "<title>" --desc-file <path>  # use - to read a multiline description from stdin
atm todo add --batch                       # read YAML/JSON items from stdin; see --help for an example
atm todo add "<title>" --creator codex     # override the detected creator; default: agent in env, else me
atm todo add "<title>" --refine            # create, then polish / split with the collection model
atm todo refine [id]                       # polish title+需求; complex work gets 分析 + child todos
atm todo refine [id] --dry-run             # print the proposal without writing
atm todo refine [id] --no-split            # polish only; never create children
atm todo start <id>                           # done/dropped 会重开并刷新生命周期时间
atm session bind <id>                           # bind this agent session; also starts todo
atm session unbind --reason scope-changed
atm todo log <id> "结果：...；证据：...；下一步：..."  # one paragraph, max 400 Unicode chars
atm todo log "结果：...；证据：...；下一步：..."       # bound session: ID is optional
atm todo log <id> "<details>" --section 分析              # route investigation/design detail out of progress
atm todo done <id> --reason "<result>"
atm todo wait <id> --wake "<condition>"
atm todo wait <id> --review-at YYYY-MM-DD
atm todo maintain <id> --limit 3
atm todo edit <id> --priority P1 --status <state>
atm todo move <id> --project <repo>
atm todo drop <id>
atm todo trash <id>                         # recoverable removal; no confirmation
atm todo list --status trashed              # inspect the trash
atm todo restore <id>                       # restore the original lifecycle state
atm todo depend add <id> <dependency-id>   # <id> waits for <dependency-id>
atm todo depend remove <id> <dependency-id>
atm todo depend list <id> --json
atm todo wake <id> --reason "<observable event>"
atm todo reconcile --json
atm todo bulk done <id>... --reason "<result>"
atm todo bulk move <id>... --project <repo>
```

会话绑定后，`log/show/doc/lint/done/wait/drop` 都可省略 `<id>`；也可写 `current`。`done`、`drop`、`wait` 会自动解绑并保留绑定历史。SessionStart hook 应使用 `atm todo match --prompt --limit 3`，不要注入完整 `atm now --json`。

`match` 的两种用途不可互换。`--prompt` 服务启动注入，总是返回 `--limit` 条候选（同项目本身加 100 分），所以它答不了「该不该新建」。查重用 `--dedup`：跨项目搜索、要求 `query_score` 达到下限（默认 30，可用 `--min-query-score` 调整）、无匹配时明确输出「可以新建」，且忽略当前会话绑定。`--json` 同时给出 `duplicate` 布尔和每条候选的 `query_score`（query 自身得分，不含项目/状态/优先级加成）。

非 JSON 模式下，单条 `atm todo add` 会把新 ID 单独写到 stdout，并把可读的 `Created <id>: <title>` 提示写到 stderr，脚本可直接使用 `id=$(atm todo add ...)`。`--refine` 是显式的：命令行添加默认不调用模型，避免拖慢 Agent 建卡和 `id=$(...)`。桌面添加在 `todo_refine_on_add`（默认 true）时会自动 `todo refine`。`todo refine` 是一次 schema 调用，不是 Agent 循环，也不会派发执行；`in_progress` 只润色不拆分。普通删除使用无确认、可恢复的 `atm todo trash`，再用 `atm todo restore` 恢复；`atm todo delete` 是永久删除并要求确认，非交互调用必须显式传 `-y/--yes`。默认不要永久删除。`atm todo prompt` 只输出文本，可以随时调用。

`creator` 记录「谁建的」，与自由文本 `source`（为什么/从哪来）正交，取值只有 `me`、`collect` 和 agent 名。创建时自动判定：环境里有 agent session 就记该 agent，否则记 `me`；连接器收集记 `collect`。环境探测不到自己的 agent（例如 CLI 不导出 session ID）时用 `--creator <agent>` 显式声明，不要让它落成 `me`。展示时 `me` 会渲染成 `atm config set owner_name <昵称>` 配置的昵称（未配置为「我」），存储值始终是 `me`。creator 字段是 v33 新增的，之前创建的 todo 保持为空，不做回填。

`atm todo log` 的默认 `进展` section 只接受单段、最多 400 个 Unicode 字符的里程碑摘要；消息里的 `tNN` 必须存在于当前或归档 todo。详细调查写入 `--section 分析`。生命周期状态、维护标签、依赖和 description 仍须用对应结构化命令更新，不能靠自由文本日志代替。`atm todo lint` 可审计历史脏数据，但不会自动改写历史动态。

## 共享记忆

```bash
atm memory recall --help
atm memory remember --help
atm memory supersede --help
atm memory forget --help
```

先 recall，再写入；写操作需要稳定事实和明确来源。

从会话沉淀时，`remember`、`supersede` 和 `forget` 都支持 `--source "session:<id>#turn:<n>"`，来源会保存在 memory event metadata 中。`atm session list --review pending` 只返回尚未整理的会话；只有写入、去重和丢弃都处理完成后，才用 `atm session review` 推进游标。

## 中央知识库

```bash
atm knowledge catalog --help
atm knowledge search --help
atm knowledge get --help
atm knowledge add --help
atm knowledge update --help
atm knowledge edit --help
atm knowledge delete --help
atm knowledge import --help
atm knowledge feedback --help
atm knowledge quality --help
atm knowledge audit --help
atm knowledge doctor --help
atm knowledge collection create --help
atm knowledge collection edit --help
atm knowledge collection rename --help
atm knowledge collection delete --help
atm knowledge collection list --json
atm knowledge bulk-edit --help
```

先 catalog/search，再 get。add/import 会修改中央知识库。

### 应用知识卡

对已确认的应用/仓库基础信息，先查重，再在统一的“应用管理” collection 中按应用或仓库名维护知识卡。

```bash
atm knowledge catalog --json
atm knowledge search "<应用名或仓库名>" --project <repo> --json
atm knowledge add "<应用名>：应用与发布信息" --file ./application-knowledge.md \
  --collection 应用管理 --producer codex \
  --project <repo> --domain release --tag app --tag pipeline
atm knowledge get <document-id> --json

# 将本次召回与 session 关联，并在使用后回填结果
atm knowledge search "<query>" --session <session-id> --json
atm knowledge feedback <document-id> --session <session-id> --outcome adopted|corrected|rejected --note "<result>" --json

# 质量聚合与只读治理巡检（不会自动归档）
atm knowledge quality [document-id] --json
atm knowledge audit --stale-days 180 --json
```

不要把稳定应用信息写入 `inbox`；已有知识卡变更时保留原文档并原地更新，而不是用 `add` 生成同标题副本。

## 外部需求收集（连接器）

```bash
atm collect status --json                                  # 连接器健康、来源、运行与处理记录
atm collect source search "<关键词>" --connector <id> --kind <kind> --json
atm collect source add --connector <id> --kind <kind> --id <external-id> [--project <repo>] --json
atm collect source add --connector <id> --kind <kind> --id <external-id> --strategy observe --interval 60 --json
atm collect source add --connector <id> --kind <kind> --id <external-id> --knowledge-collection <collection-id> --json
atm collect source add --connector <id> --kind <kind> --id <external-id> --instruction "只关注 MR 和需求" --json
atm collect source list --json
atm collect run [--source <source-id>] --json
atm collect run --due --json                       # 后台按来源独立频率运行

# 把当天沉淀（action=insight）汇总成一篇知识文档，每来源每天一篇，重跑原地重写
atm collect digest [--source <source-id>] [--date 2026-08-03] --json
atm collect digest --dry-run --json                # 只看摘要正文，不写知识库
atm collect digest --due --json                    # 后台：距上次沉淀不足 60 分钟就跳过

# 看来源原文：同时同步进本地库，不产生 Todo
atm collect history "<source-id>" --limit 50 --json
atm collect history "<source-id>" --since 2026-07-28 --limit 200 --json
atm collect history "<source-id>" --local --json     # 只读已同步的，不打网络

# 搜本地已同步的聊天（不打网络）
atm collect search "<关键词>" [--source <id|来源名>] [--sender <发送者>] [--since 2026-07-28] --json

# 处理记录本身的增删：删除只清收集侧的记录，它写出的 Todo 保留
atm collect item delete <item-id> -y --json
# 多个 id 走一个事务：要么全删，要么一条都不动（某个 id 已经没了就整批报错）
atm collect item delete <item-id> <item-id> ... -y --json
```

来源 ID 由连接器定义。连接器支持搜索时，先 `source search`，把候选连同 `detail`
念给人听，让人确认是哪一个，再用 `--connector/--kind/--id` 精确添加；不要自行挑选同名候选。

每段聊天的判定有四个出口：`create`/`append` 落到 Todo，`insight` 落到知识库（当天汇总成一篇文档），
`ignore` 是真噪音。`tasks` 来源默认每 5 分钟、四个出口都可用；`observe` 来源默认每 60 分钟，
**在配置层被限制为只能 `insight`/`ignore`**——模型判成 create/append 也会被降级成 insight，
所以闲聊群不可能替别人建任务。人显式 `collect item promote` 不受这个限制。
增量处理会把已处理消息作为 `[上下文]` 提供给模型，但只有 `[新消息]` 能触发新决策。

一段讨论会在几分钟内反复回到同一件事，每次回来都是新的一批。同一件事的新信息走
`append`，写进目标 Todo 的 `补充` 段，不再新建一条；只有确实是另一件事才 `create`，
这时 `related_todo_id` 只作上下文关联。`append` 只能落在**这个会话自己建过的** Todo 上：
手写的 Todo 或别的群的 Todo 不会被聊天改写，目标已关闭或不属于本会话时退回新建。

沉淀内容只在 `collect digest` 跑过之后才在知识库里可读；App 常驻时会跟着每次收集调用 `--due`。
需要完整聊天时仍用 `collect history`；已添加的来源可以直接用它的名字或 source-id，不必再搜一次。

取用顺序：**先 `collect search` 查本地**（不打网络、可离线、能命中历史里没成 Todo 的闲聊），
没有或要最新再 `collect history`（调用连接器并把结果同步进本地库，默认保留 90 天）。history 的 JSON
带 `synced`（本次新增几条）和 `stale`——`stale: true` 表示连接器不可用、这批是本地旧记录，
向人汇报时要说明这一点，不要当成最新状态。

## Artifact、统计与健康检查

```bash
atm artifact save --help
atm stats --days 7 --by day --json
atm stats --days 7 --by model --json  # 每行按 client + model 区分
atm stats --session <session-id> --by request --json
# 速度：上表按模型给 tok/s（模型自身生成，不含工具执行），下表按 agent 给轮次等待
# grokbuild 用它自己上报的 apiDurationMs；其余 agent 从日志时间戳推导，测不到的会单独列条数
atm stats --days 7 --by speed --json
atm quota --json                          # Codex/Grok 配额
atm quota --agent claude --json
atm quota --agent grokbuild --json
atm doctor --json
atm sync status --json
atm report today
```
