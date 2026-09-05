# ATM 设计文档

这里只写**代码里读不出来的东西**：意图、边界和非目标。

不写已实现功能清单、数据模型表或架构图 —— 那些的真相在代码里（schema 见
[`internal/store/schema.go`](internal/store/schema.go)，命令面见 `atm <命令> --help`），
手抄一份只会腐烂并开始说假话。行为细节——状态怎么流转、失败时会发生什么、数据存在哪儿——
写在 [`docs/internals.md`](docs/internals.md)。

## 定位

ATM 是一个自成一体、本地优先的多 Agent 控制台，也是用户统一的第二大脑。它从人的视角提供六个领域的能力：

1. **AI 都在干什么** — 实时看到所有 AI agent 的工作状态，历史会话可追溯
2. **干得怎么样** — AI 执行任务的效率、思考路径、是否走弯路
3. **花了多少钱** — token 用量和费用统计，按项目/agent/时间维度
4. **知识与记忆** — 所有 Agent 查询和贡献同一个中央 Knowledge，共享可追溯的 Memory
5. **协作与通信** — 交接或显式派发任务、交接产物和通知结果
6. **外部事项收集** — 本地连接器把白名单来源转成可追溯、可纠错的 Todo

项目已从 macOS 开发练手转向个人日常使用。主工作区运行在浏览器，同一个 Go 二进制通过 `serve`
提供本机页面、API 和后台职责，CLI 保持独立可用；菜单栏只保留提醒与入口，全局语音作为独立工具。
旧 Swift 主工作区只保留源码作为历史实现参考，当前 Go 不再提供它使用的 runtime 或进程间接口。
实施依据见[本地 Web 技术方案](docs/design/local-web-runtime.md)，当前交付边界见 README。

## 设计原则

- **Agent 无关**：Claude/Codex/Copilot 共享同一套数据模型，差异只存在于 parser 等 adapter。
- **单一第二大脑**：Knowledge 只有一个；domain、tag、project 是 metadata 和查询视图，不是独立知识库。
- **ATM 数据自有**：ATM 产生和管理的数据全部位于 `~/.atm`，不会静默写入项目目录或探测其他产品的私有目录。
- **显式导入**：外部知识和历史数据通过 add/import 进入 ATM，不在日常查询路径中做兼容扫描。
- **旁路而非主路**：普通 coding/chat 由客户端直接连接 Agent；ATM 停止不能阻断普通会话。
- **不实现 agent loop 或 Agent scheduler**：Go 服务可定时运行已有同步与连接器采集，但不代为启动 Agent 会话；任务交给 Codex 只用 `todo handoff`（要指针文本就 `--copy`）。与历史任务有关时只记录关联上下文，不合并事项。
- **执行必须授权且可追踪**：`todo handoff` 在 Codex Desktop 填好指针后停下，回车和审批都归人；`--copy` 只把指针交给人，由人自己开会话。ATM 不在后台启动 Agent。
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
  桌面添加默认自动跑，因为那是人随手记下的入口。App 的模型设置页通过 CLI 把 API Key 存进
  `~/.atm/credentials.json`（仅当前用户可读），Go 客户端直接读取；凭据不进入普通配置、备份、诊断包、
  argv 或日志，避免开发构建重签名反复触发钥匙串授权。模型和 endpoint 属于非敏感配置。“测试连接”复用同一客户端，以当前
  草稿配置发送最小 schema 请求但不接触 Todo。`in_progress` 只润色不拆分，
  避免把正在工作的会话解绑。
- **模块化单体，业务规则归 Application Service**（2026-08-20，2026-09-03 调整入口）：ATM 保持一个
  Go 程序和一个 SQLite；`serve` 常驻、CLI 短进程可以访问同一份数据，不拆微服务或独立读库。
  边界存在于依赖方向。Cobra、HTTP、后台 controller 和 hook 都是
  adapter，只能调用按领域划分的 application service，再由 service 调用 domain/store 和副作用 port：
  `adapter → service → domain/store`。adapter 之间禁止互相复用——尤其不能让 HTTP 调 Cobra handler，或让
  service 通过 shell 再执行 `atm`。`internal/cmd` 只保留参数解析、确认交互和人类文本/JSON 渲染；校验、
  授权、事务、幂等和跨表编排属于 service。Service 按 Work、AI Day、Collector、Knowledge、Guard、Config
  等 bounded context 划分，不建立一个接受 `string + map` 的万能 God Service。迁移可以逐域完成，
  `store` 暂时无需先拆 repository 层；当前公共调用身份和错误模型在
  [`internal/application`](internal/application)，Web 装配和白名单入口在 [`internal/apphost`](internal/apphost)；首批
  纵向样板是 [`config.Service`](internal/config/settings.go) 和
  [`aiday.Service`](internal/aiday/application.go)。
- **人的界面与 Agent 能力面可以分化，同一动作必须共享 service**（2026-08-20，2026-09-03 调整入口）：浏览器逐步成为人的主入口，Agent
  通过 skill 发现 CLI；二者的任务和授权不同，因此不要求一条 App 能力同时暴露为 Agent 命令。真正的
  一致性约束是：如果两个入口表达同一业务动作，它们必须映射到同一个 typed use case，不能各自实现规则。
  Web 通过本机 HTTP adapter 直接调用 service，不执行 Cobra 子进程；ATM Menu 只使用专门的有界本机
  control API，旧 Swift 主工作区没有当前 Go transport。普通 CLI 只保留 Agent、人工恢复或诊断确实需要的
  adapter。HTTP method 按数据或原子工作流命名，不按 screen 命名，也不把 CLI 一对一镜像成 verbs。
  现有 CLI 对 ambient environment 的识别只是 best-effort 分类，不是抗恶意调用的认证；若 Guard 要成为
  真正的安全边界，仍需 user-presence 或可验证的可信 App channel。
  本机 Web 会话只授权普通工作区操作，不能凭 `human@web`、浏览器 Cookie 或本机控制令牌开放 Guard
  批准、拒绝或规则管理。现有 Guard 人工决策约束保持不变。
- **Web 是本机工作区，原生能力按职责拆分**（2026-09-03）：页面随 Go 二进制发布，运行时无需 Node，
  不要求 WebKit 壳或另一套前端服务器。HTTP 只监听 loopback，并验证实例、浏览器会话、Host、Origin
  和写入来源。服务不可用时 CLI 继续直接使用业务层。Go 服务是唯一调度与 hook owner，关闭浏览器或
  菜单栏不会停止后台工作。语音拥有独立数据、权限和生命周期，不依赖 ATM 业务服务。当前构建只接受
  当前 schema 基线；确有下一版变更时只短期保留一阶迁移，完成唯一日用库升级后再次收平基线。
- **skill 是 Agent 的真实命令面，root help 只是人工导航**（2026-08-20）：命令存在于 Cobra 但没有写进
  ATM skill，对日常 Agent 等同于不存在。常驻 skill 只放 match/bind/context/log/wait/submit 等核心闭环；
  Knowledge、Memory、Artifact、收集纠正和历史查询按任务加载扩展说明。root help 分组仍保留，因为它让
  人工排障更容易，但它不能替代 skill 覆盖。删除 Cobra adapter 的判据是已无 Agent、人工或后台消费者，
  不能仅凭界面没有入口判断。
- **Parser 提取结构，不做业务判断**：应提取一切可用的结构化信息（summary、时间戳、工具调用），
  但不提取 git commit、不生成摘要。
- **`review` 状态保留，但不是状态前置闸门**：它表示「Agent 声称完成、人尚未验收」。`todo done` 只允许
  人执行，但不要求 Todo 必须先在 `review`——人点下完成时验收已经发生。Agent 完成实现只能 `submit`；
  删掉 `review` 等于让 Agent 自己宣布完成。
- **GUI 人工完成不强制填写结论**：点击完成/验收即生效，空结论自动留下人工 GUI 操作记录，不虚构测试证据；
  可选结论原样保留（仅去除首尾空白）。CLI 的 `done --reason` 证据要求和 Agent 禁止 `done` 的边界不变。
- **Session 镜像不主动清理**（2026-08-05 决定）：索引只增不减，没有保留期，没有后台 compaction。
  理由是它的存在意义就是比 Agent 自己的日志活得更久 —— Claude Code 三十天就清 `~/.claude/projects`，
  而 ATM 承诺 `atm stats --days 90` 的历史不会自己缩水（见
  [`docs/internals.md` 的保留策略](docs/internals.md#保留策略)）。任何自动清理都在
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

- Agent Schedule、Webhook、无人约束的循环执行或独立后台微服务；`atm serve` 只接管已有明确的本机工作，不代为启动 Agent
- 公网或局域网 HTTP 服务、通用远程 API、MCP server；本机 Web 仅开放经过审阅的工作区操作
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
- Antigravity 只提供用量，不提供会话正文（2026-08-17 核验）：token 记账在 `gen_metadata` 表里，
  逐次调用都带模型、时间戳和「命中/未命中缓存」拆分，所以 spend、会话和项目归属都是完整的；
  但对话本身是 `steps` 表里按 step 类型各不相同的 protobuf，schema 不公开，本期没有反解。同一条
  规矩：[`parser.CapabilitiesFor`](internal/parser/capabilities.go) 里它标的是 `Messages: false`，
  哪天开始解正文再改回来。只覆盖 IDE（`~/.gemini/antigravity`），CLI 的 `antigravity-cli` 不在内。
- Antigravity 的额度只有「剩余比例 + 重置时间」，上游不发布绝对配额上限也不给已用绝对值，所以
  ATM 只能显示百分比，做不到「还剩多少 token」。读数来自本机 language_server 的 loopback
  Connect-RPC（`RetrieveUserQuotaSummary`），只覆盖 Gemini 模型组：账号的另一组（Claude/GPT）是
  独立额度池，而 `QuotaInfo` 只有两个带窗口的槽位、`quota_history` 又按 `(agent, window_minutes)`
  做键，第二组无处安放且会与第一组的周窗口撞键。要完整显示需要一个支持多池的额度模型。
- Qoder、QoderWork 与 Antigravity 依赖本地 SQLite 表结构，Qoder CLI 依赖 JSONL transcript；若上游
  客户端变更 schema，需要更新 parser

## 待办

- [ ] 确认 GitHub Release 资产可从一键安装脚本下载
- [ ] 增加 clean machine 验证：`go install`、一键安装、Linux 剪贴板/通知
- [ ] 为 Copilot/Qoder 系列的实际样本补更多 parser 回归测试
