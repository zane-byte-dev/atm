# ATM 设计文档

这里只写**代码里读不出来的东西**：意图、边界和非目标。

不写已实现功能清单、数据模型表或架构图 —— 那些的真相在代码里（schema 见
[`internal/store/schema.go`](internal/store/schema.go)，命令面见 `atm --help` 和
[README](README.md)），手抄一份只会腐烂并开始说假话。

## 定位

ATM 是一个自成一体、本地优先的多 Agent 控制台，也是用户统一的第二大脑。它从人的视角提供六个领域的能力：

1. **AI 都在干什么** — 实时看到所有 AI agent 的工作状态，历史会话可追溯
2. **干得怎么样** — AI 执行任务的效率、思考路径、是否走弯路
3. **花了多少钱** — token 用量和费用统计，按项目/agent/时间维度
4. **知识与记忆** — 所有 Agent 查询和贡献同一个中央 Knowledge，共享可追溯的 Memory
5. **协作与通信** — 交接或显式派发任务、交接产物和通知结果
6. **外部事项收集** — 本地连接器把白名单来源转成可追溯、可纠错的 Todo

## 设计原则

- **Agent 无关**：Claude/Codex/Copilot 共享同一套数据模型，差异只存在于 parser 等 adapter。
- **单一第二大脑**：Knowledge 只有一个；domain、tag、project 是 metadata 和查询视图，不是独立知识库。
- **ATM 数据自有**：ATM 产生和管理的数据全部位于 `~/.atm`，不会静默写入项目目录或探测其他产品的私有目录。
- **显式导入**：外部知识和历史数据通过 add/import 进入 ATM，不在日常查询路径中做兼容扫描。
- **旁路而非主路**：普通 coding/chat 由客户端直接连接 Agent；ATM 停止不能阻断普通会话。
- **不实现 agent loop 或 Agent scheduler**：常驻 App 可定时运行连接器采集；任务来源可显式 opt-in，在新建 Todo 后派发一次 Codex，但失败不会无限重跑，Agent 也不能自行生成下一轮调度。与历史任务有关时只记录关联上下文，不合并事项。
- **执行必须授权且可追踪**：`todo prompt` 把指针交给可见会话；`todo run` 或来源上的 `auto_dispatch` 才能启动本地 Agent。每次 Run 先持久化 claim 再起进程，默认受限权限，同一 Todo 不并发执行；进程退出只形成执行证据，成功最多提交 `review`，不能替人验收为 `done`。
- **事实分域**：Todo 保存工作目标与生命周期，Git 保存实现状态，测试/CI 提供验证证据，Session 保存过程追溯；ATM 提供关联视图，不复制或覆盖其他事实源。
- **状态正交**：live activity 是观测信号，Session binding 是显式关系，Todo status 是工作生命周期；三者独立展示，禁止按项目名或 `in_progress` 猜测绑定。
- **单用户单库**：只有一个活的数据库，因此不背向后兼容成本；schema 变更的流程写在
  [`minUpgradableVersion`](internal/store/schema.go) 的注释里。

## 几个容易被重新提出的决定

- **Knowledge 不做缓存或索引**：检索每次全量读取中央 Markdown。在当前量级（约 100 篇）解析成本远低于
  进程启动，缓存反而更慢，且会引入失效问题。这个结论依赖量级，不是永久的：**文档数超过 500 篇，或
  一次 `knowledge search` 的墙上时间稳定超过 300ms 时，重新评估**。复评的对照项是索引而不是缓存 ——
  缓存要解决失效，索引只需要在写入时更新。
- **ATM 不在进程内运行模型做记忆抽取**：它只提供未整理 session 查询、带来源的 memory 写入和
  append-only review 游标；语义抽取、去重、路由与授权检查由 curator skill 约束 Agent 完成。
- **Todo refine 是内置文本模型调用，不是 Agent 循环**（2026-08-13）：人刚写下的任务常常是一句
  话。`todo refine` 通过 ATM 自己的窄 DeepSeek 客户端调用 `deepseek-v4-flash`，关闭思考模式并保留
  结构化 proposal 契约；它不再启动默认 Agent，也不回退到收集模型链。复杂时写计划并拆出子任务；
  它不派发执行、不改优先级、不发明项目。命令行 `todo add` 保持即时，只有 `--refine` 才跟着整理；
  桌面添加默认自动跑，因为那是人随手记下的入口。App 的模型设置页把 API Key 存进 macOS 钥匙串，
  只在启动 CLI 子进程时注入环境；模型和 endpoint 属于非敏感配置。“测试连接”复用同一客户端，以当前
  草稿配置发送最小 schema 请求但不接触 Todo。`in_progress` 只润色不拆分，
  避免把正在工作的会话解绑。
- **Parser 提取结构，不做业务判断**：应提取一切可用的结构化信息（summary、时间戳、工具调用），
  但不提取 git commit、不生成摘要。
- **`review` 状态保留，但不是闸门**：它表示「Agent 声称完成、人尚未验收」。`todo done` 不设前置检查 ——
  人点下完成时验收已经发生。删掉这个状态等于让 Agent 自己宣布完成。
- **Session 镜像不主动清理**（2026-08-05 决定）：索引只增不减，没有保留期，没有后台 compaction。
  理由是它的存在意义就是比 Agent 自己的日志活得更久 —— Claude Code 三十天就清 `~/.claude/projects`，
  而 ATM 承诺 `atm stats --days 90` 的历史不会自己缩水（见 README「数据源」）。任何自动清理都在
  削弱这个承诺，而且「哪条会话不再需要」只有人知道：一条三个月前的会话可能是某个决定的唯一记录。
  库因此单调增长，这是有意接受的成本 —— 单用户单机的量级下，磁盘比丢失的历史便宜。
  现有的显式手段是 `atm session forget <id>`，一次一条，且源文件还在时会直接拒绝。
  后续方向是**推荐清理**：ATM 给出候选清单和它们各自的 token/成本代价，人确认后才执行；
  不做静默的后台删除。
- **团队与多机共享不是产品目标**（2026-08-05 决定）：ATM 服务一个人的一台机器。Knowledge、Memory 和
  Todo 都是单人第二大脑，不做团队共享、不做多机同步、不做冲突合并。这不只是「暂时没做」——
  「单用户单库」「Knowledge 全量扫描」「不背向后兼容成本」这三条都建立在它之上，反悔的代价是同时推翻
  它们。因此这条不接受被需求逐步侵蚀：跨机使用请走 `atm backup` / `atm restore` 的显式搬移，
  它是一次人发起的整体迁移，而不是后台同步。

## 不做

- Agent Schedule、Webhook、独立 daemon 或无人约束的循环执行；定时发生器仍由 Enchanted 等运行客户端负责，收集自动派发只允许来源显式开启后的单次 Todo → Run
- ATM HTTP API、MCP server 或独立 Web UI
- 远程同步、团队共享与多机合并（见上一节的决定；跨机搬移用 `atm backup` / `atm restore`）
- 模型循环或 prompt/event stream 代理

## 已知限制

- Codex/Copilot 暂不支持 thinking 提取（Claude 和 Pi 支持）
- Copilot 目前偏会话检索和工具统计，不提供 token/cost 明细
- Qoder CLI 与 QoderWork 同样没有 token/cost 明细，原因在上游而非 parser（2026-08-05 核验）：
  QoderWork 的 metadata 里 `inputTokens`/`outputTokens`/`totalCostUsd` 恒为 0，而同一批消息的
  `durationMs` 与 `contextUsageRatio` 有真实值；Qoder CLI 的 transcript 里根本不存在任何
  token 字段。两者的提取逻辑保留着，上游一旦开始写入，把
  [`parser.CapabilitiesFor`](internal/parser/capabilities.go) 改回 `Usage: true` 即可恢复统计。
  Qoder 本体（IDE）提供 token，不在此列。
- Qoder 与 QoderWork 依赖本地 SQLite 表结构，Qoder CLI 依赖 JSONL transcript；若上游客户端变更
  schema，需要更新 parser

## 待办

- [ ] 确认 GitHub Release 资产可从一键安装脚本下载
- [ ] 增加 clean machine 验证：`go install`、一键安装、Linux 剪贴板/通知
- [ ] 为 Copilot/Qoder 系列的实际样本补更多 parser 回归测试
