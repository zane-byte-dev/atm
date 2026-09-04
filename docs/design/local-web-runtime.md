# ATM 本地 Web 工作区与应用拆分技术方案

日期：2026-09-03。状态：Go/Web、后台接管与两个可选原生产品的代码已落地；真实日用切换和新 bundle 权限验收另行记录。

本方案依据当前仓库源码和产品讨论编写。初始设计基线为 `c87f3cf`，本轮保留已有 t355 macOS 性能修复和其他用户改动。
历史设计取舍继续保留，具体实现以本节、后文更正以及 [Web 开发说明](../../app/web/README.md)为准。
源码、构建和隔离测试通过不等同于真实数据库已经升级、登录服务已安装或麦克风/辅助功能权限已验收。
真实切换由当前交付主线执行并补充证据，本节不提前宣称日用迁移完成。

当前实现包含：

- 同一个完整 Go 二进制提供 CLI、七个 Web 工作区、白名单 API、嵌入资源和同源 Vite HMR。
- 任务创建/编辑/生命周期、计划/进展/依赖/等待、图片选择/拖放/粘贴上传；本地知识文档 ETag 编辑、共享记忆、来源增删改、业务设置和只写凭证。
- 任务及知识草稿按编辑器实例存入 `localStorage`，关闭后可显式恢复；不会相互删除不同编辑器的草稿。
- schema 55 的创建幂等记录加上 schema 56 的持久后台执行记录、域变更版本。54/55 旧库只读打开，显式备份成功后才升级到 56。
- `jobs.run/list/show/cancel` 支持同步、采集、重处理、AI Day、额度刷新和 `todo.refine`，保留幂等身份、ETag、取消与重启中断语义。
- Go 默认接管后台：同步约 5 分钟，AI Day 约 7 分钟，采集按配置检查到期；Hook 即时接收，每 8 秒回补 presence。
- SSE 按当前页面域订阅，服务内提交主动失效；有订阅时每 2 秒检查对应域版本与文件指纹，关闭页面不停止 Go 后台。
- 用户级 LaunchAgent `serve install --print/install/uninstall`，固定当前完整二进制绝对路径，受控停机、登录启动及异常退出恢复。
- 独立 [ATM Menu](../../app/menubar/README.md) 和 [ATM Voice](../../app/voice/README.md)，各自构建、偏好域和权限；旧 Swift 主工作区保留为回退，不是新产品构建依赖。
- 原生偏好按白名单迁移：Web 的知识/来源顺序及三项用量筛选；Menu 的通知、声音与呼出快捷键；Voice 的七项语音偏好和已下载完整模型。配置凭证与转写历史不混入迁移文件。

先前只读和任务阶段已完成真实数据页面核对、隔离创建/编辑/冲突、生命周期与知识/记忆操作验证；本轮增加跨进程锁、持久执行、SSE、上传、配置冲突和原生拆分的针对性测试。
最终整合构建、真实库备份路径、服务身份、登录项状态及实际原生权限结果应随上线记录补充，不能沿用早期测试数量代表本次最终验收。

## 1. 决策与范围

ATM 从 macOS 开发练手项目回归个人日常工具：目前服务一个人在一台 Mac 上的使用，优先减少维护成本、缩短界面迭代反馈，并保住已有数据和工作流。

目标架构确定为：

- 同一个 `atm` Go 二进制提供 CLI 和 `serve` 两种运行方式；CLI 执行后退出，`serve` 提供页面、API 和后台工作。
- 完整工作区由浏览器承载，前端编译资源随 Go 二进制发布，不需要 Swift/WebKit 主窗口。
- 菜单栏作为可选的轻量伴随 App，读取 Go 服务状态，提供通知和入口；退出它不停止后台工作。
- 全局语音输入成为独立 App，拥有自己的快捷键、模型、设置、权限和发布生命周期。
- CLI、Web 和菜单栏复用同一套 Go application service、同一份本地数据。CLI 不因服务未运行而失效。

第 15 节保留分阶段实施依据；任务、工作区、后台与独立原生产品均已有实现，剩余交付边界是实际切换和系统权限验收。

不新增团队账号、云端部署、多机同步、远程监听、微服务、Agent 执行循环或通用任务调度平台。Web 服务允许后台执行的仍是已有同步、收集等明确工作，不负责启动 Agent 会话或替用户批准外发动作。

### 1.1 对旧设计决定的调整

原 [DESIGN.md](../../DESIGN.md) 排除了 HTTP API、独立 Web UI 和常驻协议进程。这些决定以原生 App 作为主入口为前提，现已按用户的个人使用目标调整为 Go 本机服务与浏览器工作区。

仍保留：模块化单体、`adapter → service → domain/store`、单用户单库、本地优先、CLI 查询默认只读、Agent 提交与人工验收分离、普通会话不依赖 ATM。

DESIGN.md 已更新相应决定并链接本方案；各运行入口和构建边界以当前代码为准。

## 2. 当前实现与迁移依据

| 当前实现 | 已有基础 | 迁移含义 |
| --- | --- | --- |
| [CLI 入口](../../cmd/atm/main.go)、[Cobra root](../../internal/cmd/root.go) | 加载全局配置、初始化定价、命令级记账 | 增加长期运行入口，审计所有“进程退出时收尾”的假设 |
| [application](../../internal/application/call.go) | Actor、Origin、RequestID、稳定错误分类 | HTTP 增加明确来源，不从请求 JSON 接受身份 |
| [appipc](../../internal/appipc/server.go)、[ipc](../../internal/ipc/registry.go) | 按领域绑定 typed use case；IPC 使用 stdin/stdout | 复用业务和 DTO，HTTP 不执行 CLI，也不套用固定的 human@ipc |
| [Work](../../internal/work/service.go)、[Session](../../internal/session/service.go)、[Knowledge](../../internal/knowledge/service.go) 等 | 大量业务已收敛到 service | 页面迁移不重写规则；残余 cmd 编排按迁移功能下沉 |
| [SQLite 打开逻辑](../../internal/store/db.go) | WAL、写连接 busy timeout、只读入口、schema 检查 | 保留多进程访问；新增常驻连接、锁和迁移生命周期约束 |
| [Swift Store](../../app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift) | 定时同步、收集、配额、状态、通知准备、页面缓存 | 分别移到 Go runtime 或前端，不能整体翻译成一个 React 全局 store |
| [事件监听](../../app/macos/Sources/ATMMenuBarApp/ATMAgentEventListener.swift) | 接收 `~/.atm/notch.sock`，start/stop 会 unlink socket | 必须单一接收者切换；旧 App 与新服务不能直接抢占同一路径 |
| [关注提醒](../../app/macos/Sources/ATMMenuBarApp/ATMAgentAttentionNotifier.swift) | 维持状态轮询、状态音和阻塞通知 | 当前源码已移除常驻刘海绘制；README 有历史描述，刘海不列为必须迁移的当前功能 |
| [内置模型记账](../../internal/cmd/builtin_model_usage.go) | 模型调用暂存在内存，CLI 结束时落库 | `serve` 必须周期/按批次落库，不能无限积累直到服务退出 |

[性能排查](../performance-audit-2026-09-03.md)和[修复记录](../performance-fixes-2026-09-03.md)已经说明：全量统计、无关状态更新和长文重复计算会放大界面成本。Web 必须继承按域加载、范围查询、取消和缓存预算；换渲染技术不保证这些问题消失。

## 3. 目标结构

```mermaid
flowchart TB
    Browser[浏览器：ATM Web]
    Menu[可选菜单栏 App]
    CLI[atm CLI：短进程]
    Voice[独立语音 App]
    HTTP[atm serve：HTTP / SSE]
    Runtime[后台运行管理：同步 / 收集 / 事件 / 提醒]
    Services[共享 Application Services]
    Data[本地 SQLite / Markdown / 配置 / 凭据]
    Hooks[Agent hooks]

    Browser <-->|同源 HTTP / SSE| HTTP
    Menu <-->|本机控制 API| HTTP
    HTTP --> Services
    Runtime --> Services
    CLI --> Services
    Services --> Data
    Hooks -->|现有事件协议| Runtime
    Runtime -->|状态失效 / 提醒事件| HTTP
    Voice --> VoiceData[语音独立设置 / 模型 / 系统权限]
```

Go 服务的生命周期独立于所有界面。HTTP 和后台管理处在同一进程；无需独立 worker 服务或消息中间件。

### 3.1 功能归属

| 功能 | 唯一责任方 | 界面职责 |
| --- | --- | --- |
| Todo 生命周期、计划、绑定、依赖、归档 | Work service | Web 提交意图并显示结果 |
| 会话索引、历史、搜索、统计 | Go services + runtime | Web 按页面/范围查询 |
| 知识、记忆、源文件导入与回写 | Go services | Web 编辑、预览与冲突处理 |
| 自动同步、外部收集、登录退避 | Go runtime | Web 展示状态并发起手动执行 |
| Agent hook 接收、状态合并、过期处理 | Go runtime | Web/菜单栏订阅相同状态 |
| 需要提醒的条件、去重、已处理状态 | Go notification coordinator | 伴随 App 或备用渠道负责显示 |
| 页面路由、选择、滚动、临时草稿 | Web | 不回写成业务事实 |
| 菜单栏图标、原生通知、全局呼出入口 | 伴随 App | 不扫描会话、不执行采集调度 |
| 全局语音、模型下载、跨 App 输入 | 独立语音 App | 不依赖 ATM 服务 |

## 4. CLI 与服务生命周期

### 4.1 命令设计

当前完整构建支持：

```sh
atm todo list                  # 现有 CLI，直接访问同一业务层
atm serve                      # 前台常驻：HTTP + 后台工作
atm serve --open               # 启动服务，并打开已授权的浏览器页面
atm serve status --json        # 查询当前数据目录对应实例
atm serve stop                 # 停止对应实例，不按模糊进程名杀进程
atm serve install --print      # macOS：预览登录服务
atm serve install              # macOS：安装并启动当前完整二进制
atm serve uninstall            # macOS：卸载登录服务，保留数据
atm serve --background=false   # 不接管后台的页面实例；不等同于只读
```

- 不采用全局 `--headed`：是否启动长期服务与是否打开浏览器分别表达。
- 默认绑定 `127.0.0.1:47321`，前台实例支持 `--port` 指定及 `0` 自动分配；LaunchAgent 要求固定端口。
- 同一数据目录只允许一个 runtime。实例锁按规范化后的数据目录持有整个进程生命周期，PID 文件本身不是锁。
- 已有对应实例时，`atm serve --open` 只申请打开该实例的页面，成功后退出。普通 `atm serve` 报告已运行并退出，不再启动第二套定时任务。
- 被无关程序占用的端口应明确报错，不能把那个页面当 ATM 打开。实例信息需验证版本、数据目录标识和随机实例 ID。
- `status` 不触发同步、迁移数据库或读取业务正文。服务不存在时也能明确返回 `running=false`。
- `serve` 不等待首轮全量同步完成才提供页面；前端显示已有数据、各域新鲜度和最近错误。

### 4.2 实例运行文件

实际 ATM 数据目录使用 `runtime/`，目录权限 0700，敏感文件 0600：

```text
runtime/
  server.lock         # OS 文件锁
  server.json         # PID、instance_id、origin、版本、启动时间；无凭据
  control.token       # 当前实例的本机控制令牌，启动轮换
  notification.json   # 去重游标/显示渠道状态，不存原始会话全文
  presence-owner.json # Go Hook 所有权/租约；停止后保留接管决定
  serve.stdout.log    # 登录服务输出
  serve.stderr.log    # 登录服务错误输出
```

已接受后台执行和幂等结果保存在业务 SQLite，纳入备份和迁移；不依靠临时 JSON 恢复执行事实。运行文件不包含业务正文。`presence-owner.json` 在停机和卸载后仍保留，避免旧 App 在 Go 重启窗口擅自恢复后台，回退必须显式处理所有权。不能拿旧文件的 PID 直接发送信号：先通过本机控制接口核验实例，只清理仍属于自己的 socket。

### 4.3 关停、重启和自启动

收到退出信号后停止接收新写请求和新后台工作，取消可取消的读取，等待已开始的事务/当前批次完成，落模型用量和状态，再关闭 SSE、HTTP、socket 和数据库连接。设置总关停预算，但不能把尚未完成的外部操作标为成功。

已提供用户级 `atm serve install --print`、`serve install`、`serve uninstall`。安装记录当前完整二进制的绝对路径、固定数据目录/端口和环境 PATH，不复制文件或安装系统级 daemon；launchd 使用 RunAtLoad/KeepAlive 和 10 秒退避，关停预算 45 秒。输出日志写到数据目录 `runtime/`。重新加载已停止服务执行 `serve install`，没有单独 `restart` 子命令。

`serve stop` 对托管实例先卸载当前登录会话中的 job，保留 plist 供下次登录，避免 KeepAlive 立即拉起；`uninstall` 同时移除登录配置，保留数据和 Go owner 标记。不覆盖无关 LaunchAgent，已有未托管实例须先停止。登录服务的实际安装与异常恢复仍需在目标账户验证。

## 5. Go 业务复用与 HTTP 适配

### 5.1 依赖方向

```text
CLI 参数解析 ────────────────┐
旧 IPC typed binding ───────┼→ application service → domain/store
HTTP typed handler ────────┤
runtime 明确后台工作 ──────┘
```

HTTP handler 只处理路由、身份、参数、限制、结果编码。它不能调用 Cobra handler、执行 `atm _ipc` 子进程，或把业务校验复制成 TypeScript 规则。

`internal/apphost` 已负责 Web 依赖装配、配置 gate、运行时挂接和按域调用；CLI/旧 IPC 继续复用同一业务服务。业务服务不反向依赖 apphost。

当前采用显式方法白名单和 typed service 输入/输出，按需复用 appipc 中已有 DTO；没有引入通用命令执行路由或额外的 `appcontract` 包。

### 5.2 接口形态

采用小规模 typed RPC：`POST /api/v1/<method>`。延续已有领域方法名，降低迁移映射成本，但只公开经审阅的白名单。POST 读操作在契约中仍标记为 query；不能因都用 POST 就允许客户端自动重试写操作。

| 接口 | 用途 | 处理方式 |
| --- | --- | --- |
| `GET /healthz` | 最小存活检查 | 不携带业务正文或凭据 |
| `GET /api/v1/bootstrap` | 版本、模式与能力 | 写入/后台能力由服务端装配与 schema 决定 |
| `POST /api/v1/todo.list`、`todo.show`、`todo.doc` | 任务及详情 | Work 读取服务，有界列表 |
| `todo.create/update/start/done/archive/restore` | 任务生命周期 | 同一业务服务、ETag 和创建幂等账本 |
| `todo.plan.set/progress.append/dependency.add/dependency.remove/wait.update/wake` | 计划、进展、依赖和等待 | 显式字段与生命周期校验 |
| `session.list/search/show/status`、`presence.snapshot` | 会话与运行状态 | 分页正文、索引新鲜度与独立 Hook overlay |
| `knowledge.catalog/query/document.get/document.create/document.update/collection.create`、`memory.*` | 知识与记忆 | 逐方法白名单；受管文档跨进程版本保护 |
| `usage.snapshot`、`quota.cached`、`day.snapshot/show/ledger` | 统计、额度缓存、AI Day | 查询不执行模型或外部采集 |
| `collect.overview/items/item.show/history` 及来源/已读/归档写接口 | 收集管理 | 来源身份字段固定，执行另走 jobs |
| `settings.get/business.save/credential.save/credential.delete` | 设置 | 修订保护、业务字段白名单；凭证只写 |
| `GET /api/v1/events?domains=todos,presence` | SSE 失效 | 仅白名单域，不接受对象/路径/凭据参数 |
| `POST /api/v1/jobs.run/list/show/cancel` | 明确后台执行 | 持久 Job、幂等键、状态/取消，不是 ATM Todo |
| `POST /api/v1/tasks/{todo_id}/images` | 任务图片上传 | 单文件 multipart、expected_etag、格式/大小限制 |
| `GET /api/v1/attachments/{id}` | 授权附件读取 | 业务对象解析路径，无任意文件接口 |

表内省略前缀的方法均采用 `POST /api/v1/<method>`。原设计中的通用 `config.*`、`dashboard.snapshot`、
`local.open`、任意文档上传和 `session.timeline` 没有整体开放；实际白名单以 `internal/web/methods.go` 为准。

现有 `todo.list` 已有 limit/offset，不代表所有列表均已达到前端所需的分页和摘要成本；按功能补齐真正缺失的 service 查询。

### 5.3 契约与错误

HTTP 采用独立 `api_version`，不借用旧 IPC 的版本握手值。保持稳定的 `request_id/data/error` 形态，但省掉只对 stdin/stdout 有意义的 envelope 层级。

```json
{
  "api_version": 1,
  "request_id": "web-…",
  "data": {"todo": {"id": "t123", "title": "示例"}, "etag": "…"}
}
```

成功只出现 data，失败只出现 error；返回 `invalid_argument → 400`、`not_found → 404`、`conflict → 409`、`forbidden → 403`、`busy/unavailable → 503`、`internal → 500`。传输额外状态包括 401、413、415、429。错误保留 field、当前版本等可用详情，不输出内部堆栈、凭据和任意外部响应正文。

RequestID 用于追踪；另设 `Idempotency-Key` 处理可以安全重复提交的写操作，二者不混用。相同幂等键必须校验方法及正文摘要，键复用但参数不同返回 conflict。按实际提交边界处理：

- Todo 创建：幂等记录与 Todo 在同一 SQLite 事务提交，重复请求返回同一对象。
- 后台执行：在 SQLite 持久化 queued 记录及稳定 job ID 后返回 Job；普通 RPC 成功响应只表示已接受，执行结果另查状态。同一键与请求恢复同一执行，不依靠 `jobs.json`。
- 文件写入：Markdown 与 SQLite 不是一个原子事务。当前文档编辑使用跨进程锁、ETag 和原子文件替换；不声称已有通用文件/数据库两阶段恢复平台。断连后先读取当前结果，再决定是否以新版本保存，不能自动重放会新建一份文件的动作。

前端禁用重复按钮只改善交互，不能代替这些服务端规则，也不能用纯内存 map 声称跨重启幂等。

TypeScript DTO 维护 Go 公开契约的小型明确映射，不把整个 store 结构自动暴露给浏览器。稳定 ID 使用字符串；时间沿用各领域契约，新 Job 使用 RFC3339，已有领域仍有 Unix 时间戳，由各自格式化入口处理。

## 6. 常驻进程必须补齐的基础能力

### 6.1 配置并发

当前 config 以 package 全局变量/map 为主，部分保存后调用 LoadConfig；已有文件锁保证磁盘读改写，不保证 HTTP 并发读与 reload 安全。

首个可用版本增加 runtime 级 RW gate：普通 service 请求和后台工作持读锁，配置保存/reload 获得写锁后重建捕获旧配置的 service 依赖。所有入口必须经过 gate，包括内置后台任务和文件变更触发的 reload；专用配置 handler 不再嵌套进入普通 handler，避免死锁。

配置写侧使用非阻塞 TryLock：有操作在读配置时，人工保存返回 busy 并保留输入，外部 reload 由定时器稍后重试。不能直接排队等待 RWMutex.Lock，因为等待中的写者会阻止后来的读者，把一个慢作业放大成整个界面读取阻塞。网络作业设置时限，文件写入也有明确结束条件；拿到写锁后的更新区段保持短小。

这是有意的过渡：持续长任务可能延迟配置生效，但不能冻结其他查询。后续只有这项限制影响使用时，再改成不可变 ConfigSnapshot + provider，使每次操作持有一个配置代次。

外部 CLI 改配置时，runtime 通过配置文件签名检查后排队 reload。data_dir、监听地址和数据加密等启动级配置只能重启生效；重新加载需要从默认值重建，避免删除配置键后留下旧全局值。定价变化同时使对应统计缓存失效。

### 6.2 SQLite、事务与多进程

- 保留 CLI 直接读写同库，不把 HTTP 服务变成唯一允许访问数据的入口。
- runtime 在启动阶段检查 schema；旧库只读，迁移由显式 `serve migrate` 在备份和执行锁保护下完成，不由 HTTP 请求或后台工作隐式升级。
- 只读 HTTP 请求不得走隐式同步或建库；无索引时返回可展示的状态，后台初始化明确走写入口。
- 常驻查询和变更监测使用正常 WAL 可见连接，不能使用 `immutable=1` 回退；现有只读 fallback 在沙箱中可能忽略未 checkpoint 的 WAL，不能成为 Web 的长期视图。
- 增加可注入的 DB opener/provider 时保留连接所有权：当前许多 service 会 defer Close，不能把共享池直接塞进去再被一次请求关闭。先维持安全的独立句柄，确认需要优化后再统一生命周期。
- 写事务保持短小；连接器、模型调用、文件下载和系统进程执行不在 SQLite 写锁内完成。已有 Work 事务和 effect/outbox 按其所有权继续使用。
- runtime 同类后台工作去重，并为 sync/collect 加覆盖 CLI 的跨进程工作锁。进程内 singleflight 不能阻止另一个 `atm sync` 同时运行。
- 两个页面或 CLI 同时编辑同一文档时，写入携带 `expected_etag`，在 service 持有对应事务/文件锁后比较当前内容；冲突保留草稿并展示差异，不静默覆盖。etag 不能只用秒级时间戳。

### 6.3 模型用量与副作用

拆开 CLI 命令结束记账与 runtime 记账。常驻模型调用在服务调用结束或短周期批量落库，设置缓冲上限并在关停时尽力 flush；写失败记录可诊断状态。不要让一次 serve 运行变成持续数天的单条 CLI 调用统计，HTTP/后台工作分别记录动作和耗时。

数据库提交后执行外部副作用沿用已有 effect 机制。前端请求断开不代表提交失败；服务端返回结果前断连时，通过幂等键/对象状态核验。无法确定执行结果的外部动作标记 unknown/interrupted，禁止自动再次执行。

### 6.4 后台执行管理

只管理明确类型：同步、收集、任务整理等现有操作。耗时操作在 SQLite 提交执行记录后返回 Job（包含 id、kind、status、phase），通过 SSE/查询显示 queued/running/succeeded/failed/canceled/interrupted；不把 HTTP 成功响应当作执行已完成。近期展示状态有界保留，持久记录按恢复需要保留，服务重启把未完成项标成 interrupted；可重复的索引同步可以按策略重跑，有外部副作用的工作必须先查已有结果。不要把索引同步称为只读操作。

超时按操作设置，取消传播到支持 context 的接口。现有不接受 context 的扫描需逐步补足；完成前只能停止下一批，不能承诺浏览器取消即可立即中断所有底层操作。

## 7. 后台调度与事件

### 7.1 已实现的后台节奏

以下是当前实现的默认间隔，不是性能保证：

| 后台工作 | 触发 | 约束 |
| --- | --- | --- |
| 会话同步 | 启动和约每 5 分钟；用户可手动发起 | 共享跨进程执行锁，避免 Web/CLI 双重同步 |
| Agent 活动 | Hook 立即更新，每 8 秒回补 presence 与配置 | 与浏览器可见性无关，过期状态会清理 |
| 外部收集 | 用户开启后按现有配置间隔 | 保留连接器登录失效退避、mute 和游标语义 |
| 配额/今日摘要 | 按需＋共享短 TTL；历史采样随 sync | 菜单栏只拿轻量摘要，不触发全范围统计 |
| AI Day | 启动和约每 7 分钟更新当前结果；用户可重建所选日期 | 历史详情按需，不随每次 presence 变化重算 |
| 关联评审证据等慢查询 | 页面订阅或用户触发，保持现有节流 | 不因为存在 server 就把原按需能力全部设成定时任务 |

读取接口不能通过请求中的 `sync=true` 绕开上述调度；HTTP 契约把同步明确拆成写操作。

### 7.2 Hook socket 切换

初期保留 `notch.sock` 的路径和发送协议，避免要求所有 Agent 集成同时升级。名字沿用不意味着还需要刘海 UI。

Go 成为唯一接收者，移植现有事件解析、snapshot/stream、事件顺序、过期与状态合并规则。hook 无接收者时仍快速返回，保持旁路性质。先核验已有 Go 事件存储/发送能力，再只迁移缺失的接收和合并逻辑。

保留 `ATM_NOTCH_SOCKET` 测试/配置覆盖；当前发送端有约 400 ms 交付上限，不扩大为长时间等待服务。Go 已实现 attention overlay、busy/completion 转换、过期与查询回补；重启后先建立状态基线，不补发历史完成提醒。回补周期固定 8 秒，未照搬 Swift 的窗口可见性调度。

不能在 Go 启动时无条件 unlink 已存在的 socket。先用明确模式完成旧 App 停用/退出，再由实例锁和存活核验接管。旧 App 如需并存，必须先修改为连接 Go 的客户端，禁用其 listener 的 start/stop/unlink 路径；否则旧 App 退出也可能删除新服务的 socket。

### 7.3 SSE 与外部变更发现

SSE 只发送小型事件，例如：

```text
id: <instance_id>:<sequence>
event: resource.changed
data: {"domains":["todos"]}
```

服务内成功提交后发布 `resource.changed`，presence 和 job 使用各自的域。前端据此刷新相关 query，不能用 SSE 复制全部数据库或整段 transcript。

订阅使用 `/api/v1/events?domains=todos,presence` 声明当前页面域，域名白名单校验；当前没有对象 ID 订阅参数。
路由变化重建连接，后台标签页释放连接，同域消费者共享一次变更检查。Go 的 Hook 和 presence 回补独立运行，
不依赖浏览器订阅；ATM Menu 使用专门的本机控制接口读取轻量 snapshot/通知 feed。

SSE 支持自动重连、心跳和有界 replay buffer。收到不属于当前实例或已超出 buffer 的 Last-Event-ID 时发送 reset，让客户端重新查询当前页面相关域；认证失效另走重新连接流程。慢客户端不能阻塞后台发布；队列满时断开并重连重建。多标签页按连接数设置温和上限，首期无需 SharedWorker。[SSE 机制](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events)

**CLI 写库必须被发现。** 首期保留 CLI 原路径，通过固定的一条 `sql.Conn` 每约 2 秒检查 `PRAGMA data_version`；其值只能与同一连接过去的值比较。不能在连接池里每次随机借一条连接做比较。[SQLite 文档](https://sqlite.org/pragma.html#pragma_data_version)

data_version 只表示疑似变化。schema 56 的 `workspace_changes` 按域触发修订计数，CLI telemetry 不会让无关业务域全部失效；旧只读库回退到 data_version。当前订阅域另外检查 Todo/知识/config 等固定目录的内容指纹，覆盖同长度修改、原子替换和删除，不跟随任意符号链接。无订阅不执行这轮检查，也不借变更发现重算全量统计。

统计使用短 TTL/同步完成事件使当前查询范围过期，回到前台再校验；未打开的统计范围只标记 stale。共享记忆存储在 SQLite，归 memory 域失效；知识 Markdown、Todo 文档和配置文件需单独监测相关路径的签名。目录变动、原子替换、同长度改写和删除都要覆盖。文件通知可用于加速，但仍保留低频校验与页面重新聚焦校验。

## 8. 前端实现

### 8.1 技术选型

当前采用 React + TypeScript + Vite、React Router、TanStack Query，样式使用共享 CSS variables 和按工作区加载的 CSS。没有引入 SSR、Next.js、Redux 或通用低代码框架。

这是针对已有 Go 后端、浏览器内交互和单人维护的选择；依赖版本在实施时选择相互兼容的稳定版本并锁定，不在文档里追逐 patch 版本。Vite 可以构建可直接静态托管的资源。[React 工程说明](https://react.dev/learn/build-a-react-app-from-scratch)、[Vite 构建文档](https://vite.dev/guide/build.html)

复用现有信息架构和常用操作语义，不逐像素重做原生控件。表单、可访问对话框、Markdown 等在实际使用时引入成熟组件，避免为了减少依赖重新手写复杂焦点系统。

### 8.2 页面与状态

```text
/tasks                  /tasks/:id
/agents                 /agents?session=...
/knowledge              /knowledge?collection=...&document=...
/collection             /collection?source=...&item=...
/usage
/ai-day
/settings
```

详情 ID、可分享/收藏的筛选条件进入 URL。服务端支持这些页面路径的 SPA fallback；`/api/*`、`/assets/*` 的错误必须返回真实错误，不能 fallback 成 HTML。

- 服务数据：按领域 query key，例如 `['todo', id]`、`['usage', range, agent]`；不建包含所有字段的全局 store。
- 临时交互：组件本地 state。列表选中不会让整份业务缓存重新排序。
- 偏好：主题、知识/来源顺序与用量筛选存在浏览器本地存储；属于客户端，不自动成为 Go 配置。窗口和分栏宽度采用响应式布局，未复制旧原生几何参数。
- 草稿：按对象与编辑器实例持久保存在 localStorage，任务保留 ETag、基线与幂等身份。跨标签页/重启恢复需要明确选择并复制记录；提交只清理自己的对应快照，不能删除其他编辑器的草稿。知识与记忆也提供独立恢复入口；存储失败会明确提示。
- 刷新：有缓存时先显示，后台只重验相关 query。HTTP 客户端支持 AbortSignal；取消旧读取和按 ID 存放响应，避免快速切换时旧结果覆盖新选择。
- 默认重试显式配置：普通读取最多少量退避重试，验证/身份错误不重试，写操作默认不自动重试。TanStack Query 的默认 stale/refocus/retry 行为需要按此目标覆盖。[查询默认行为](https://tanstack.com/query/latest/docs/framework/react/guides/important-defaults)

当前任务、会话、知识、记忆各有搜索，不依赖 Spotlight。跨域统一搜索仍是可选交互设想，不属于已交付接口；需要时可复用这些独立读取，不引入新的索引事实源。

### 8.3 大列表、长文、图片

任务先加载活动集摘要，完成历史和归档按需分页；列表数量增长后再启用虚拟列表。服务端查询也必须有边界，前端只隐藏 DOM 不会减少全量 SQL、JSON 和排序成本。

会话正文分批加载，不要求全部挂在 DOM 上。Markdown 使用禁用原始 HTML 的共享渲染组件；独立全文下载、内容 hash 解析缓存和复杂代码高亮仍是按需优化方向，不属于本轮已交付能力。

图片通过授权附件 ID 读取，上传接受 PNG/JPEG/GIF，单张最多 10 MB、每任务最多 10 张，支持选择/拖放/粘贴。前端不把 Base64 大图塞入列表或全局状态；服务端验证实际格式及像素边界。专门缩略图生成仍不是当前接口，不能据此宣称已经优化所有大图成本。

## 9. 本机访问、身份与外部内容

这部分处理的是本地 Web 新增的实际入口，不引入账号系统。

### 9.1 本机访问会话

- 只监听 loopback，并严格校验 Host 为当前实例允许的主机和端口，防止 DNS rebinding；不提供 `0.0.0.0` 作为个人版选项。
- `--open` 通过本机控制凭据申请短时、一次性的 bootstrap ticket，放在 URL fragment；页面读取后立即清除 fragment，再 POST 交换成 HttpOnly、SameSite=Strict 会话 cookie。ticket 不写日志、Referer 或浏览器本地存储。
- 普通收藏的页面在会话仍有效时直接进入；服务重启/会话失效显示“重新连接”，引导使用 `atm serve --open`，不自动从公开 GET 接口发放凭据。
- HTTP API、附件和 SSE 均要求访问凭据。Cookie 有效期、bootstrap 有效期和 companion token 生命周期有界；服务启动轮换控制令牌。
- POST 校验 Content-Type、Origin（完整 scheme/host/port）、Fetch Metadata 和会话 CSRF token；不允许通配 CORS。缺少浏览器来源信息的 native 请求必须使用专门的本机 bearer 凭据。
- Go 1.25 已提供 CrossOriginProtection，可作为一层保护；它允许 safe methods 且不是身份认证，不能替代上述校验。[Go 文档](https://pkg.go.dev/net/http#CrossOriginProtection)

本机 token 是访问能力，不是用户在场证明；同一 OS 用户能运行任意代码的威胁不靠 localhost/cookie 消除。本文不会把浏览器登录、Origin 或 HTTP header 当作“必定是人点击”的证据。

### 9.2 业务身份与 Guard

增加 `OriginWeb`，由服务端构造 Call；浏览器请求不能设置 ActorKind、Origin、Agent session 或任意绑定身份。后台任务使用 controller 身份。

Todo 人工完成沿用 GUI 产品语义：认证的交互入口点击完成留下 Web 操作记录，Agent CLI 仍只能 submit。Work.Done 已显式支持 OriginWeb 的 GUI 结论规则，并保留对应校验；不伪装 OriginCLI/IPC 绕过业务逻辑。

**Guard 决策与工具安装/规则管理不随 IPC 方法整体开放。** 当前 [Guard service](../../internal/guard/service.go) 与 [management](../../internal/guard/management.go) 要求人类 CLI 来源。当前 Web 和轻量 Menu 不开放 Guard 决策/工具安装，保留真实 CLI 操作路径。以后需要 Web 完整审批时另行设计人类决策入口，禁止简单把 `OriginWeb` 加入放行列表。

### 9.3 内容与本地文件

Markdown 默认禁用原始 HTML/脚本，限制 URL scheme；外部消息和模型回复按不可信内容渲染。生产资源全部本地打包，使用 CSP，禁止把第三方网页放入拥有本机 API 凭据的同源界面。

附件读取根据业务对象查路径并校验实际解析后的文件范围，处理 `..`、符号链接与替换；不能开放 `GET /file?path=/任意文件`。上传已采用当前图片数量/大小约束和实际图片格式校验，不接受 SVG/HTML 等可执行内容。

图片导入支持用户选择文件上传副本；知识原文导入仍使用已有 CLI。浏览器不会提供可用于回写的真实绝对路径；当前已有源文件关联由 Go 保留并负责回写。新增需要持续回写源文件的导入可继续走 CLI 的明确路径参数，或后续增加受约束的本地选择器，不伪称网页上传等价于原生路径导入。

连接器/模型密钥由 Go 管理，前端只显示 configured 状态；模型凭证修改是只写请求。Web 没有开放 `local.open`、连接器登录命令或任意 shell 执行 API。连接器仍由用户在原有工具中登录，之后显式重试采集。

原生来源跳转的历史实现保留在 [ATMAgentSessionLauncher](../../app/macos/Sources/ATMMenuBarApp/ATMAgentSessionLauncher.swift)：子会话返回根会话、Terminal/iTerm 按 TTY 定位、IDE 按工作目录打开。当前 Web 负责会话阅读和任务关联，未复制这些需要 macOS 自动化权限的动作。以后确有需要时只提取明确的固定动作，元数据不足时给出降级说明。

## 10. 菜单栏伴随 App

菜单栏使用轻量原生菜单：提供服务状态、今日用量、当前任务与缓存额度摘要、打开 ATM、同步并刷新和伴随 App 自身设置。任务和额度的完整详情，以及 Agent、收集等工作区页面进入 Web。它不持有完整工作区，不扫描 SQLite/会话目录，不运行第二套 sync/collect，也不加载语音模型。

它通过经过规范化校验的数据目录实例信息和 control token 连接固定 loopback API，每 10 秒获取通知 feed 与有界摘要，显示后单独 ack。服务端只返回菜单需要的计数和标量统计，不发送任务正文或完整 presence 会话。额度读取不执行 provider，菜单发起的同步只会排队 Go runtime 已有的 `session.sync` job。

摘要分区失败时返回局部错误，不能中断通知租约。服务不可用时保留最后一次摘要并显示连接错误，自身退出不停止服务。完整任务编辑、生命周期、统计筛选和业务设置继续由 Web 承担，避免复制第二套完整 Swift 工作区。

刘海目前已不是现有源码的功能，只有后续明确需要时才作为这个 App 的显示能力添加。

### 10.1 通知唯一归属

Go 负责决定“发生什么、是否需要提醒”，显示渠道采用配置/租约选一个：伴随 App 在线时由它展示；否则按配置使用 Go 调本机通知工具或仅保留 Web 未读。首期浏览器只展示站内提醒，避免多标签页重复弹系统通知。

备用通知渠道按实际能力降级：当前 osascript fallback 没有稳定 ID 撤回能力，只发送一次普通“有待处理事项”提醒并提供 Web/CLI 入口。实时待处理状态以 Go 为准，不承诺旧系统横幅可以撤回；伴随 App 负责稳定 ID 的更新与撤回。渠道恢复不补播历史完成提示音。

通知标识来自业务对象和状态转换，伴随 App 使用稳定系统通知 ID 更新/撤回。Go 保留去重游标；启动时先建立当前状态基线，不把历史完成事项全弹一遍。伴随 App 的显示确认与业务已读分离。

CLI 的 Todo 生命周期通知已优先转交 Go owner，通过有确认回执的固定协议共享去重键。存在 Go owner 标记时，即使交付结果不确定也不另发 CLI 横幅，避免重复提醒；从未接管的独立 CLI 仍使用原备用通知路径。停止服务不会悄悄恢复另一套显示渠道。

不能承诺跨崩溃严格 exactly-once 展示。明确采用稳定 ID、短期去重和已读状态恢复；结果不确定时宁可保留待处理项让用户查看，也不自动重复触发业务动作。

## 11. 全局语音 App 拆分

语音拆分是独立交付线，范围包括录音协调、快捷键、Apple Speech/SenseVoice 路由、模型下载、语音浮层、文本清理、跨 App 粘贴、失败后复制恢复和相关设置。现有代码可搬移，不需要先重写引擎。

独立 App 拥有自己的 bundle ID、UserDefaults 域、Application Support/模型目录、日志和版本。首次设置页提供旧偏好和已下载完整模型导入；复制到目标目录并验证 SHA256，目标已有完整模型则保留。不删除旧数据，不导入转写历史。

系统麦克风、语音识别、辅助功能授权不能假定随代码复制而继承；新 bundle 必须按真实系统流程重新获得权限。热键注册也要在旧语音模块关闭后接管，不能让两个 App 同时持有同一组合键。

ATM 的业务数据不迁到语音 App，语音不读取 ATM 数据库。新 Go/Web/Menu 构建已隔离语音依赖，`sherpa-onnx-spm` 只用于独立 Voice target。旧 `app/macos` 中的源代码和资源保留作为回退，不在本轮删除已有用户工作。

### 11.1 具体迁移清单

代码范围是 `ATMVoiceInput`、`ATMVoiceInputCoordinator`、`ATMVoiceTranscriberRouter`、`ATMAppleSpeechTranscriber`、`ATMSenseVoiceTranscriber`、`ATMSenseVoiceModel`、`ATMVoiceTextCleanup`、`ATMVoiceOverlay`、`ATMTextInjector`，加上通用热键代码和对应测试。`sherpa-onnx-spm` 只进入语音 target。

正式 App 当前的 UserDefaults domain 是 `dev.zanebyte.atm.menubar`。首次迁移只复制以下白名单，并记录迁移版本；Debug 的偏好域需单独识别，不假设等于正式版：

```text
ATMVoiceInputHotKeyEnabled
ATMVoiceInputHotKey
ATMVoiceInputEngine
ATMVoiceInputLanguage
ATMVoiceInputOnDeviceOnly
ATMVoiceInputRemoveTrailingPeriod
ATMVoiceInputDictionary
```

当前模型目录为 `~/Library/Application Support/ATM/VoiceModels/SenseVoiceSmall-int8-2024-07-17/`，包含 `model.int8.onnx` 与 `tokens.txt`。迁移通过临时目录复制、完整性验证后原子改名，避免网络重复下载或中断后留下半个模型。当前最近转写只在内存，不存在必须自动搬迁的转写历史。

热键 manager 按 target 明确允许的 action：伴随 App 只有 launcher；语音 App 只有 voiceInput 和录音期间的 cancelVoice。不能直接把会注册全部枚举 action 的旧 manager 同时放进两个 App。

验收保留短按松键、模型初始化期间松键、Esc 取消、录音时限、Space/前台 App 切换、权限拒绝、剪贴板恢复，以及取消后过期结果不能注入新目标。语音 App 在 ATM 服务和菜单栏全部退出时仍要完整可用。

权限文案和界面必须与实际引擎一致：模型缺失时存在 Apple Speech 回退，且本地识别是独立设置，不能原样复制旧资源中的“音频不上传”断言。展示实际使用的引擎和本地设置，分别验证本地模型、Apple 本地模式及允许远程识别时的行为。

### 11.2 其他原生偏好如何处理

实际提供 `app/macos/Scripts/export-web-preferences.swift [--dev]`，导出 `kind: atm-native-preferences`、`version: 1` 和明确的正式/开发 bundle ID。
浏览器在“设置 → 外观”选择 JSON、预览勾选后确认，只读取以下五项：

| 原生键 | 导出字段 | Web 使用方式 |
| --- | --- | --- |
| `ATMKnowledgeCollectionOrder` | `knowledge_collection_order` | 知识集合排列；忽略已删除 ID，新条目附后 |
| `ATMCollectionSourceOrder` | `collection_source_order` | 采集来源排列，规则同上 |
| `atmUsageFilterModel` | `usage_filter_model` | 模型明细和对应时间桶筛选 |
| `atmUsageFilterClient` | `usage_filter_client` | 规范化为已有 Agent 筛选 |
| `atmUsageFilterProject` | `usage_filter_project` | 项目汇总/趋势优先，延续原生语义 |

文件最多 2 MB，排序数组最多 1,000 项，单值最多 512 字符；未知字段、错误版本和格式直接拒绝。
导入不上传到 Go，只影响当前浏览器；不同浏览器分别导入。模型/项目并无联合桶，界面明确项目优先，不能假算交叉结果。
通知开关、每类音效/音量与呼出热键由 Menu 导入，七项语音偏好和完整模型由 Voice 导入。
窗口尺寸、旧分栏宽度、折叠、滚动、缓存采用新的网页布局；业务 config.json 继续共用，不复制凭证或转写历史。

## 12. 目录与依赖组织

```text
cmd/atm/                       # 一个 Go 二进制入口
internal/cmd/                  # CLI adapters、serve、LaunchAgent 装配
internal/apphost/              # 共享依赖装配、配置 gate、变更指纹
internal/web/                  # HTTP、鉴权、SSE、图片上传、开发代理
internal/background/           # 持久执行队列、同步/采集/Day/refine 调度
internal/presence/             # Hook、presence、通知状态与渠道
internal/<domain>/             # 现有 application services
internal/store/                # SQLite、业务文件、schema 56
app/web/src/                   # 路由、任务、草稿、API、主题、SSE
app/web/src/workspaces/         # 六个其他工作区及偏好迁移
app/web/public/                # 静态资源
app/web/dist/                  # Vite 生成资源，不手写
app/web/assets_embed.go        # webui 构建标签 + go:embed
app/web/assets_stub.go         # 无 webui 时的明确降级
app/menubar/                   # 独立 ATM Menu，无旧主界面依赖
app/voice/                     # 独立 ATM Voice，无 Go 数据依赖
app/macos/                     # 保留回退源码和已有 t355 改动，不参与新产品构建
```

先在当前仓库内明确构建目标和依赖边界，避免同时引入多仓库发布协调。语音是独立产品边界，不意味着首日必须完成仓库搬家。

## 13. 开发、构建与发布

### 13.1 本地开发

Vite 提供 HMR，Go 提供 API。已实现浏览器始终访问 Go 的同一个 origin：开发模式用 `--dev-ui http://127.0.0.1:5173` 把非 API 请求代理给 Vite，并配置 HMR WebSocket 走同一开发入口。明确代理白名单，生产包不启用此模式，不因开发方便放开生产 CORS。

开发与日用实例不要共享同一数据目录并运行两套后台工作。使用 `--data-dir` 指定空工作区或脱敏 fixture 副本，`--background=false` 关闭开发实例调度；不通过改 HOME 重定向其他工具的数据和凭据。该开关不禁止写入，涉及生命周期、收集、通知测试仍使用隔离目录。

### 13.2 单二进制资源

前端 `npm ci` + build 生成 app/web/dist；Go `//go:embed all:dist` 嵌入编译产物，运行时无需 Node。构建必须检查入口存在及资源引用完整。[Go embed 文档](https://pkg.go.dev/embed)

为避免“没有 dist 时连 Go 单测/go install 都无法编译”，使用显式 `webui` build tag：

| 当前构建入口 | 产物/行为 |
| --- | --- |
| `make build` / `make install` | 构建前端，再以 `-tags webui` 构建完整 atm |
| `make build-cli` / 默认 `go install` | 保留无需 Node 的 CLI 构建；serve 无打包页面时给出明确安装完整包提示 |
| 前端开发模式 | stub 构建可以代理指定 Vite 服务，API 可独立开发 |
| Release / goreleaser | 必须先构建前端并带 webui tag，产物检查失败则停止发布 |

README 已区分默认 go install 与完整发布包。Release 的资源与 Go API 一起更新，实例重启后重新建立浏览器会话并加载页面，不另建远程前端发布系统。

index.html 使用 no-cache，带内容 hash 的资源可长期缓存；API/敏感附件默认不进共享缓存。第一版不启用 Service Worker，避免升级后旧页面缓存继续调用新 API。页面资源离线可用，连接器和远程模型能力仍取决于各自网络。

### 13.3 备份、升级和回退

已实现显式升级：schema 54/55 的 `serve` 实例仅开放读取，不接管后台。切换前 `serve stop`、退出旧 macOS App、暂停并等待旧写入，让日用 CLI 和所有调用方使用同一份支持 schema 56 的完整二进制，再执行 `serve migrate`。命令同时取得同步/采集执行锁并在 `backups/` 保存升级前归档，备份成功后才升级至 56，随后 `serve --open` 或 `serve install` 接管。自定义数据目录保持一致，新建空工作区不需要迁移。已接管后不能重新开启旧 App 调度。

页面迁移不改变 Todo、Session、Knowledge 的事实归属。需要新增幂等记录等辅助 schema 时，复用现有迁移和版本拒绝规则，先备份，不把 schema 降级交给前端处理。

回退以“支持同一 schema 的旧界面/二进制”为前提；若旧版本不认识新 schema，应停止写入、另存新数据并恢复明确的备份，不能宣称任意旧二进制可直接打开新库，也不自动丢弃备份后的修改。

## 14. 验证与观察

### 14.1 必须通过的功能验收

| 场景 | 成功标准 |
| --- | --- |
| 只用 CLI，服务未启动 | 原命令和 Agent 集成继续可用 |
| 关闭所有浏览器窗口 | 同步、收集、hook 状态仍运行；无界面连接不触发完整统计 |
| 退出菜单栏 | Web 可用，后台工作继续，通知按既定渠道策略降级 |
| CLI 改 Todo/绑定 | 活跃 Web 页面在外部变更检测窗口内更新，已有编辑草稿不被覆盖 |
| 两处编辑相同对象 | 后提交者明确得到冲突，不丢前一次修改 |
| 任务新建/编辑/归档/恢复/人工验收 | 生命周期、绑定关闭、审计、副作用与 CLI/现有 service 一致 |
| 快速切换任务、慢响应、断网 | 旧结果不覆盖新选择，错误与最后成功数据可区分 |
| 重启服务、SSE 断线、实例变化 | 页面重建当前域状态，不靠无限保存历史事件恢复 |
| 重复启动/停止旧 App | 只有一个 hook 接收者/同步调度者，不误删新 socket |
| 模型调用后长期运行 | 用量及时可查，缓冲有界，退出不是唯一落账时机 |
| 外部网页请求本机服务 | 无法读取业务数据或触发写入；附件无任意路径入口 |
| Guard | Web 不借本机 token 或伪造来源放宽现有人工决策约束 |
| 语音拆分 | 在目标常用 App 中按住/松开、取消、权限拒绝、粘贴失败均可恢复 |

### 14.2 性能目标与测量方法

使用同一台 Mac、同一份可重复数据、Release Go 与生产前端；对照当前优化后的原生版本，不能只与旧 Debug 全量快照比较。

建议初始目标：缓存内任务选择反馈 p95 不超过 100 ms；已运行服务的任务页首批可交互内容 p95 不超过 1 s；CLI 普通变化在约 3 s 内反映到可见页面；hook 已到达服务后的可见状态更新目标 500 ms 内。大规模同步期间分别记录劣化值，不把正常情况下的目标写成所有场景保证。

测试数据包含当前真实量级、约 2,000 条任务、长 Markdown/图片、长会话和高频 Agent 事件。记录 API/解析/渲染各阶段耗时、传输体积、Go 内存、浏览器相对空白基线的增量内存、空闲 CPU、重复操作后缓存占用。

内存和启动成本先建立基线再定数值门槛；本方案没有已测得的 Web 性能收益。核心要求是不存在无消费者全量计算和无界缓存。

### 14.3 测试边界

复用现有 Go 业务测试，补 HTTP 契约/权限/输入限制、配置并发 race、双进程 CLI 与服务竞争、幂等/冲突、真实 socket 接管、SSE 恢复。前端只为有状态风险的流程写必要测试，使用浏览器端到端覆盖任务闭环和草稿恢复，不测试每一个低风险样式。

日志按请求/工作关联 ID 记录方法、域、耗时、错误类和结果状态，不记正文与密钥。常驻诊断输出增加调度健康、最后成功时间、队列长度、监听实例、缓存与 SSE 连接数，复用 ATM diagnose，避免另造监控平台。

## 15. 实施阶段与退出条件

| 阶段 | 范围 | 退出条件 |
| --- | --- | --- |
| P0：确认基线与职责 | 固定当前可用版本/备份，记录常用流程，更新设计边界；列清旧 App 后台开关与 socket 所有权 | 能明确启动/停止某一套后台，不发生双采集；已有性能工作不被覆盖 |
| P1：只读 Web 骨架 | apphost、serve、配置 gate、loopback 会话、静态资源、bootstrap、任务列表/详情；CLI 保持原路径 | 从完整二进制打开真实数据；输入校验和身份不靠模拟；无需 Swift 主窗口 |
| P2：任务闭环 | 创建/编辑/生命周期、并发条件、幂等、草稿、附件、按域失效/SSE；模型记账先于开放整理/标题生成；sync/collect 锁先于开放相应手动执行 | 日常任务管理可在 Web 完成；CLI 并行写入可见且不覆盖草稿；模型用量不积压；手动执行不撞旧后台 |
| P3：接管后台 | 接管调度、hook 接收与状态合并、通知路由；先完成旧 App 客户端/仅语音模式或停用 | 关浏览器/菜单栏后后台仍正常；无重复同步、socket 抢占、通知双发；旧语音可独立保留 |
| P4：完整工作区 | 会话、知识/记忆、收集管理、统计、AI Day、设置、来源跳转；补必要后台自启动 | 常用工作无需回旧主窗口；长文、上传、源文件回写边界清楚 |
| P5：原生产品拆分 | 提取菜单栏伴随 App；语音独立 target、偏好/模型导入及权限验证 | 两个 App 各自运行/退出；语音不依赖 ATM 服务；仅保留有用入口 |
| P6：收尾 | 新产品构建脱离旧 Swift 主工作区与语音依赖，整理文档/构建/安装；保留旧源码和 t355 既有改动作为回退 | 主产品使用一套 Web 工作区；旧源码不参与新构建，日用切换和权限验收证据明确 |

上述阶段仍是切换顺序，而非同时运行两套后台的许可。代码状态：P1/P2/P4 已有完整 Web 操作，P3 已有 Go 调度/Hook/通知与执行锁，P5 已有独立 Menu/Voice 及白名单导入，P6 已完成产品构建隔离和文档调整。
实际退出条件仍需部署现场核验：真实库备份升级、稳定二进制路径、唯一后台 owner、登录服务以及新 App 的系统权限。

旧普通模式发现 Go owner 标记时拒绝恢复后台；回退源码另提供真正的仅语音分流，在建立旧 Store/监听器之前完成。
`ATM_MENU_ONLY` 只隐藏主窗，仍不能作为停后台协议。当前产品已提供独立 Voice，无需把旧主 App 作为语音常驻依赖。
旧 Swift 与 t355 工作保留，不通过删除用户改动来宣称 P6 完成。

## 16. 风险与实施时仍需核验的事项

| 风险/待核验项 | 处理决定 |
| --- | --- |
| 并发服务暴露旧全局状态竞态 | 配置 gate 和副作用/记账审计先于后台全面接管 |
| CLI 与 HTTP 数据可见性不一致 | 正常 WAL 连接、外部变更监测、页面重新聚焦校验 |
| 统计仍因任何写入重算 | 按域指纹、按需范围、TTL；CLI telemetry 不等同于统计全量刷新信号 |
| 双启动抢 socket、重复收集/通知 | 单一 owner、跨进程工作锁、渠道切换和旧 App 兼容开关 |
| 原生文件路径导入无法等价搬到浏览器 | 上传副本与关联源文件明确区分，保留 CLI 路径导入 |
| 语音权限/模型路径迁移 | 首次导入与独立授权验收，成功前保留旧资源 |
| 浏览器资源占用/体验不符合预期 | 同机生产构建实测，不承诺自动比 Swift 更快 |
| 现有公开安装方式只构建 Go | 明确完整包与 CLI-only 构建区别，发布时资源缺失必须失败 |
| 功能迁移膨胀成新平台 | 保持一个 Go 单体、一套数据、一套 Web 主界面，原生部分可选 |

2026-09-03 初始核对发现的前置问题已有对应实现：

- SyncAll/SyncAgent 使用数据目录级跨进程锁，保护增量 offset 到提交的整个执行范围；收集执行/重处理共用锁，迁移也持有这些锁。
- `internal/background` 使用 schema 56 的持久执行账本；模型调用结束落用量，任务 AI 整理在模型调用前后都核对 ETag，不覆盖并行编辑。
- `internal/presence` 保留现有 Hook 协议，拒绝接管活跃外部 socket，只删除自己的 socket；8 秒回补与生命周期不依赖窗口。
- 旧 App 通过 Go owner 标记避免恢复调度；Menu 只消费轻量状态/通知，Voice 不连接 ATM 数据。通知采用唯一显示渠道与稳定 ID，备用横幅不保证撤回。
- 日用切换仍必须统一实际调用的二进制、等待旧子进程结束、备份升级并确认系统权限。仅在独立目录构建测试用 `atm` 不代表旧 App 或日用 CLI 已改用它。

最终日用入口为完整 `atm` 的浏览器工作区与 Go 服务，可选 Menu 提供持续提醒，Voice 独立使用。
实际数据库、LaunchAgent、偏好导入和权限验收的完成状态由交付主线另记，本方案不提前代替这些结果。
