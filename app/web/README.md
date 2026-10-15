# ATM Web 开发

ATM 的主界面由浏览器承载，同一个 Go 二进制提供 CLI、Web API、嵌入页面和后台服务。七个工作区按需加载，
无需 Swift 主窗口。当前代码已接入以下范围；真实日用库升级、登录服务安装和原生系统权限验收由上线步骤单独确认，
不能把构建或隔离测试通过视为已经切换日用环境。

| 工作区 | 已实现的操作 |
| --- | --- |
| 工作板（任务） | 列表/看板/表格/泳道、组合筛选、排序与属性显示、浏览器保存视图、弹窗详情与文档、新建编辑、生命周期、计划、进展、依赖、等待条件、图片上传及明确发起 AI 整理 |
| 收件箱（收集） | 待处理结果与详情、来源筛选、已读/归档，以及二级入口中的来源管理、处理记录、本地消息历史、手动采集及重新处理 |
| Agent | 会话列表与分页对话、按需展开筛选、会话信息与工具记录，以及筛选弹层中的手动同步 |
| 知识 | 集合/文档搜索与新建、集合拖动排序与管理（重命名/说明/移动文档后删除）、本地文档原地编辑及版本冲突、外部文档副本、共享记忆检索/新建/修订 |
| 统计 | 日期、Agent/模型/项目筛选，趋势与明细，缓存额度及手动刷新已配置的额度来源；徽章日历按月查看 AI Day 结果 |
| 消息 | 默认未读、操作通知历史、详情、运行中任务取消、全部已读和清除记录；此浏览器保留最近 200 条通知，同一任务按最新状态更新 |
| 设置 | 五种主题、业务偏好、模型地址与名称、只写凭证管理、索引状态、后台能力和会话同步 |

“设置 → 外观”提供极简黑白、石墨雾蓝、鼠尾草绿、暖砂和午夜五种主题，默认极简黑白。
选择通过当前站点的 `localStorage` 保存，刷新后保留并同步同源标签页；存储不可用时仍可在本次浏览期间切换。
外观设置保存在浏览器，不依赖后端配置写入。公共样式由 `src/app.css` 统一组织，主题变量集中在 `src/themes.css`，基础尺寸在 `src/style.css`；
各页统一页头、留白、卡片、指标和按钮。收集、会话和知识的列表/详情分别滚动，窄窗口提供返回入口。

中栏的布局与样式集中在 `src/components/workspace-navigator.tsx` / `.css`：Agent、知识、统计和设置共用
`WorkspaceSplitLayout`、`WorkspaceNavigator` 与 `WorkspaceNavigatorHeading`，设置和统计分类复用
`WorkspaceNavigatorItem`。工作板和收件箱也复用标题区；任务视图、会话行和知识树保留各自的内容结构。
桌面列表/树导航宽度使用 `--workspace-navigator-width`，菜单类导航为 220px；760px 以下按菜单、树或列表分别处理导航。
Agent 会话和收件箱条目共用 `WorkspaceListItem`，统一标题、辅助信息、时间、悬停与选中样式；
进展摘要、未读圆点和失败状态作为可选内容保留。

通用按钮、输入框、选择器、日期、提示、通知、折叠区和弹层使用 Ant Design，共享当前主题。
后台操作由全局通知观察器持续跟踪，切换页面后仍更新反馈；通知历史和运行中的任务操作集中在“消息”页。
运行与同步展示 ATM 索引、后台能力开关，不是操作系统资源监控。

任务、Agent、统计按路由分别加载，各有错误边界。任务入口为 `src/features/tasks/task-workspace.tsx`。Agent 与统计复用 `features/agents/activity-query.ts` 的状态查询，格式化在 `src/format.ts`，前台可见性在 `src/use-foreground.ts`。页头组件只引入自己的 `workspace-title.css`，公共样式由 `src/app.css` 统一组织。

新建任务会自动记住当前填写的标题、说明、项目和优先级，再次打开时自动填入，创建成功后清除。
多窗口以最后一次填写为准，较早的创建响应不会删除较新的输入；创建重试仍使用原来的幂等身份。
编辑已有任务不保存或恢复浏览器内容，重新打开时读取服务端版本；并发编辑冲突仍需核对合并。
知识编辑器的保存机制独立。浏览器存储失败会显示提示。

知识侧栏和集合管理共用排序逻辑，提供拖动与上下移动按钮，顺序保存在当前浏览器。集合重命名仅改显示名称；非空集合须选择接收集合，移动文档后才能删除，Web 接口不支持强制删除文档。任务列表、看板与泳道共用任务卡片，看板和泳道使用状态列头与独立横向滚动区域。

图片支持文件选择、拖放和粘贴，限 PNG/JPEG/GIF、单张 10 MB、每任务 10 张。上传逐张附带当前 ETag，
服务端检查实际图片格式、尺寸/数量和任务版本；读取通过授权附件接口，不接受任意本机路径。
本地知识文档使用跨进程锁和 ETag 原地保存；有外部来源的文档创建副本，持续关联源文件的导入仍走 CLI。

页面使用 SSE reset 刷新当前路由的查询；服务内提交只排入一次合并信号，由后台扫描后通知，写响应不等待目录读取。
有订阅时每 2 秒检查所有业务域版本及固定工作区文件的指纹，复用未变文件的内容摘要，每分钟完整校验内容。
事件正文为空，不推会话正文；每个连接只保留一个待处理 reset，重连时实例或序号不匹配便重新查询。后台标签页断开，回到前台重新查询；轮询仍作兜底。

`serve` 接管会话同步、自动采集、AI Day、Agent Hook 和通知路由。
本地会话按配置时区每晚 23:00 后同步一次，次日 06:00 前启动或唤醒可补采，同一晚重启不重复执行；
默认仅 Codex 参与自动同步，通用偏好可勾选辅助来源或全部取消；历史仍可搜索和手动同步。
每个来源串行处理并独立持久化去重；AI Day 随已启用来源同步成功后更新。
外部采集仍按来源的到期规则执行；实时状态通过 Hook 推送，不再常驻扫描会话文件。
关闭页面不停止这些工作。手动操作通过白名单 `jobs.run/list/show/cancel` 执行，包括 `session.sync`、`collect.run`、
`collect.reprocess`、`day.rebuild`、`quota.refresh` 和 `todo.refine`。执行记录落 SQLite，有界队列、取消、超时和重启中断状态；
读取页面不会自行提交手动作业。慢模型、连接器和外部进程操作使用启动时的配置快照，等待期间释放配置读锁，设置保存可继续；新操作使用新配置。
模型配置和凭证管理不会试调模型；凭证不回传页面、不写入浏览器存储。

CLI 和 Web 复用业务服务、同步/采集执行锁及模型记账；Go 负责通知事实、去重和渠道路由。
可选 [ATM Menu](../menubar/README.md) 负责原生显示，独立 [VoxCaret](../voice/README.md) 负责全局语音。
旧 Swift 主工作区已移除，源码及性能改动保存在 [Git 历史](https://github.com/zane-byte-dev/atm/tree/f96ed66f35dbd099de5ab16dbbea008c60381e27/app/macos)。
API 不提供任意命令、配置键、Guard 审批或任意文件读取入口。

## 前端代码结构

按业务归组，通用组件和基础设施独立存放。导入具体模块，不通过集中导出文件把整个业务域带入页面。

```text
src/
  main.tsx, workspace-shell.tsx  启动、懒加载路由、应用外壳
  api.ts, api-contracts.ts       请求传输与 API 契约
  query-domains.ts, live-*.ts*   缓存失效与实时刷新
  components/                   跨页面 UI：导航、列表项、表单控件、反馈、Markdown
  features/
    tasks/                      工作板、详情、检查器、编辑、操作与查询模型
    knowledge/                  知识目录、阅读、编辑、草稿与分页
    collection/                 收件箱、来源管理、上下文与采集状态
    agents/                     会话、对话与活动查询
    usage/                      用量、额度、徽章日历
    settings/                   外观和业务设置
    notifications/              消息存储、后台任务模型、观察器与操作
  preferences/                  浏览器偏好存储及订阅
  styles/                       跨业务表单样式
```

- 页面入口负责路由、选择状态和子组件编排。知识页的目录树、详情、创建表单和草稿存储分别在 `knowledge-tree`、`knowledge-details`、`knowledge-composers`、`knowledge-draft`；任务详情、检查器和新建/编辑弹窗分别在 `task-detail`、`task-inspector`、`task-editor-dialog`。
- 同一业务的类型、查询、纯函数和 CSS 与组件放在一起。跨页面复用的 UI 放 `components/`，不从通用组件反向导入某个业务页面；后台任务的类型与状态定义在 `runtime-job-model.ts`，不依赖通知组件。
- 优先复用 Ant Design 和现有公共组件。错误统一用 `Notice`，普通加载用 `Loading`，带慢请求提示和重试的查询用 `QueryLoading`。任务状态标签统一在 `task-status.ts`，Agent 名称统一在 `format.ts`。
- 有多个实际调用方、语义与行为一致时再提取公共能力。任务与知识的草稿策略、不同业务的分页规则保持各自归属。
- 功能样式随模块加载；全局样式的顺序由 `app.css` 管理。现有全局兼容样式仍集中在 `style.css`、`workspace-style.css` 和 `web-improvements.css`，修改时检查跨页面影响。
- `npm run check` 检查类型及未使用的局部变量/参数；纯逻辑回归由 `npm test` 运行，交互使用 `tests/*.browser.js` 的隔离 fixture，不在日用数据上执行写入测试。

## 构建完整程序

需要 Go 1.25+、Node.js 24 和 npm。从仓库根目录执行：

```sh
make build
bin/atm serve --open
```

`make build` 串行执行 `npm ci`、TypeScript 检查、Vite 生产构建和带 `webui` 标签的 Go 构建。
页面嵌入二进制，运行机器无需 Node、npm 或源码目录。`make dist` 和 GoReleaser 使用相同的嵌入方式。
在 macOS 上，构建脚本优先选用钥匙串中的 Apple Development 身份，没有时回退为 ad hoc；可用
`ATM_CODESIGN_IDENTITY` 指定证书或 `-`。

默认 `go build`、`go install` 和 `make build-cli` 不带 `webui`，无需前端依赖或 `dist` 也可编译。
这类产物可以使用 CLI；启动新的 Web 实例时会提示安装完整构建。

`make build` 和 `make build-cli` 默认都输出 `bin/atm`。若该路径正在被日常 CLI 或 Go 服务使用，
开发时指定独立目录并保留可执行文件名 `atm`，避免覆盖日用 CLI，也兼容按文件名授权的本机安全策略：

```sh
NPM_CONFIG_CACHE=/private/tmp/atm-web-npm-cache make build BIN_DIR=bin/atm-web
bin/atm-web/atm serve --data-dir /private/tmp/atm-web-data --port 0 --open
```

`NPM_CONFIG_CACHE` 可选；这里使用单独缓存，避免默认 npm 缓存的权限问题。`--data-dir` 只适用于
`serve` 及其子命令，不改 HOME。每个 `serve` 实例都会启动 Go 后台职责并允许业务写入；写入验收使用
空目录或脱敏副本，切勿把临时测试实例指向日用库。

## CI 与交付检查

仓库根目录的 `scripts/release-check.sh` 提供以下独立阶段：

| 命令参数 | 检查范围 |
| --- | --- |
| `go` | Go 全量测试、vet、无需前端资源的 CLI 构建及发布配置契约 |
| `web` | 前端测试、TypeScript 与 Vite 生产构建、嵌入页面的 Go 二进制及版本 |
| `dev` | `node --test scripts/dev.test.mjs`，覆盖开发服务接替、编译失败保留、恢复与退出清理 |
| `native` | macOS 上两个 Swift 包的测试、Release `.app` 组装、资源与权限说明、语音录音 entitlement、签名 |
| `release`（默认） | 全部通用阶段及 Darwin/Linux/Windows 的 amd64、arm64 六平台交叉构建；macOS 上还执行 `native` |

PR CI 在 Linux 分别执行 `go`、`dev`/`web`，macOS 只执行 `native`。标签发布先通过独立 macOS 原生门禁，再执行完整通用检查与六平台交叉构建；不在普通 macOS CI 重复 Go/Web 全套。

```sh
GOCACHE=/private/tmp/atm-go-cache ./scripts/release-check.sh go
./scripts/release-check.sh native
```

检查使用 `bin/atm-release-check.*` 下的唯一临时目录保存可执行文件与 App，结束后清理，不覆盖日用 CLI 或现有 `dist` App。单独组包时，Menu 可设 `ATM_MENU_OUTPUT_DIR`，VoxCaret 的构建与打包可设 `VOXCARET_OUTPUT_DIR`；默认仍输出各自 `dist`。CI 使用 ad hoc 签名，不依赖开发证书。

组包检查不会打开 App。通知授权、麦克风/Apple Speech/辅助功能授权、按住/松开快捷键、Esc、前台切换及真实目标 App 内输入仍由用户实机验收；自动测试与签名通过不代表这些系统权限已经获得。

## 页面修改与验证

```sh
npm run check --prefix app/web
npm run test --prefix app/web
npm run build --prefix app/web
mkdir -p bin/atm-web-check
go build -tags webui -o bin/atm-web-check/atm ./cmd/atm
./scripts/codesign-local.sh bin/atm-web-check/atm
```

修改后重新构建、停止旧实例，再启动新二进制。生产资源在编译时嵌入，运行中的程序不会自动加载新
`dist`。同一数据目录只运行一个实例；换了可执行文件也应先停止旧实例：

```sh
bin/atm-web-check/atm serve stop --data-dir /private/tmp/atm-web-data
bin/atm-web-check/atm serve --data-dir /private/tmp/atm-web-data --port 0 --open
```

浏览器第一次连接和会话到期后通过 `serve --open` 建立会话。有效会话由本地持久密钥签名，Go 实例
重启或重新构建后仍可继续使用；直接打开从未授权的地址会显示连接提示。
生产资源的完整端到端验证仍使用上面的嵌入构建；日常页面调整可以使用下面的同源热更新。

### Go 同源热更新

从仓库根目录一键启动：

```sh
make dev
```

它启动独立的 Vite 和 Go 进程，日志分别带 `[ui]`、`[api]`、`[build]` 前缀。
默认沿用日用 CLI 的本地数据目录（包括配置的 `data_dir`），页面修改直接写入真实数据，不生成样例任务。
首次编译和签名成功、Vite 就绪后，通过 `serve stop` 接替同一数据目录的现有服务，再打开开发页面。

- React/CSS 保存后由 Vite 热更新，无需重新构建 Go。
- Go 源码、`go.mod`、`go.sum` 或后端资源变化后自动编译并签名；成功后重启后端。
- 编译或签名失败时保留旧后端，修正并保存后自动重试。编译期间的后续修改也会再次构建。
- 浏览器只在首次启动时打开；重载沿用端口和登录数据。后端重启期间 API 会短暂不可用。
- Ctrl+C 停止本次启动的前后端及构建进程。退出后不会自动恢复先前的日用服务；需要时运行日用 `atm serve --open`。
- 数据库版本不兼容时服务会报错，不自动迁移真实数据。

默认 Vite 端口 `5173`，Go 端口 `47321`。也可显式选择隔离目录用于写入测试：

```sh
make dev DEV_UI_PORT=5174 DEV_PORT=47323 DEV_DATA_DIR=/private/tmp/atm-my-dev-data
```

分别调试时使用两个终端（先启动前端）：

```sh
make dev-ui    # 终端一：只启动 Vite
make dev-api   # 终端二：构建、接替并自动重载 Go 后端
```

只改样式时保持 `dev-api` 运行即可，前端可独立重启。`DEV_OPEN=` 禁止自动打开浏览器。
浏览器使用 Go 打开的地址（默认 `http://127.0.0.1:47321`），不直接访问 Vite 端口。
HTML、模块和 `/__atm_hmr` WebSocket 均由 Go 转发到 Vite，业务 API、连接票据、Cookie 和 CSRF
校验仍由 Go 处理。开发模式不要求构建 `dist`；监听脚本使用已有的 Node 运行时，无额外工具依赖。
开发入口要求固定且非零的 Go 端口。

验证开发进程的接替、失败保留、恢复和退出清理：

```sh
node --test scripts/dev.test.mjs
```

`--dev-ui` 仅接受带端口的 `http://127.0.0.1` 或 `http://[::1]`，不能指定主机名、凭据、路径或查询参数。
开发代理不会转发 ATM 的 Cookie、认证或控制头，也不会接管 `/api` 和 `/healthz`。仅开发模式放行
React Refresh 的内联初始化脚本，并限定 WebSocket 为 Go 同源；未传该参数时继续使用原生产 CSP。
最终验收请移除 `--dev-ui`，使用完整嵌入构建确认页面可离线运行。

应验证真实任务闭环、CLI 并行修改、编辑冲突、草稿恢复、创建重试、归档恢复和服务重启。
生命周期写入只在隔离数据目录进行。页面不提供 Guard 授权或任意本机文件读取入口。

`call` 对 `todo.*`、`knowledge.*` 方法推导输入输出，`todo.list` 按 `workbench`/`metadata_only` 区分返回；动态任务写入使用 `callTodoMutation` 或 `callScopedTodoMutation`。`npm test` 包含类型负例与 Go 实际 JSON fixture 对照。修改 Go 返回结构时同时核对 TypeScript 契约，并显式更新 fixture：

```sh
UPDATE_WEB_API_FIXTURES=1 go test ./internal/apphost -run '^TestWebAPIWireContracts$' -count=1
```

布局与分页浏览器回归入口为 `tests/workspace-layout.browser.js`、`tests/task-pagination.browser.js`，使用各自隔离 HTML fixture，通过 `playwright-cli run-code` 执行。前者覆盖页面懒加载、设置/详情断点和统计视图切换，后者覆盖分组分页、全范围选择和刷新行为。

## 数据基线与登录服务

当前 schema 为 v58，唯一保留的一阶迁移是 v57 → v58。它为任务增加持久化的 `updated_at`，首次按完成时间与已有任务文档修改时间的较大值回填，不重写文档。`serve` 不会隐式迁移旧库：遇到 v57 会拒绝启动并提示以下流程，迁移命令在执行前创建备份；更早的 schema 需通过支持它的历史版本处理。

```sh
atm serve stop
atm serve migrate
atm serve --open
```

以上是需要切换日用数据时才执行的操作；构建、CI 和隔离回归不会执行它们。只读查询不迁移数据库；常规 CLI 写入口使用同一条 v57 → v58 迁移实现。已是 v58 时迁移命令报告无需迁移。

旧版二进制不能直接打开更新的库。回退前停止所有写入并另存新数据；前端不做 schema 降级。
所有步骤使用自定义数据目录时均传入相同 `--data-dir`。新建空工作区会直接创建当前 schema。

macOS 用户级 LaunchAgent 使用执行安装命令的那个完整二进制的绝对路径，不复制二进制：

```sh
atm serve install --print        # 预览 plist，不写文件或启动进程
atm serve install               # 安装并启动，固定端口默认 47321
atm serve --open                 # 打开已运行服务的授权页面
atm serve stop                  # 当前登录会话中卸载 job，保留 plist，下次登录恢复
atm serve uninstall             # 移除登录服务；保留业务数据与 Go owner 标记
```

安装支持 `--data-dir` 与固定 `--port`，不能用随机端口 0。已有未托管实例先 `serve stop`；
需要重新载入已停止的登录服务时再执行 `serve install`。日志位于数据目录 `runtime/serve.stdout.log` 和
`runtime/serve.stderr.log`。服务以当前用户登录身份运行，异常退出由 launchd 拉起。

工作板四种模式共用筛选与排序，切换模式不会清除条件。泳道按项目分行、状态分列；列表可按状态、项目分组或不分组。保存视图包含布局、筛选、排序、密度和属性显示，存于当前浏览器 localStorage；不保存新建任务信号。

工作板由 SQLite 对整个筛选范围查询计数、筛选项和分组，随后只读取可见分组的任务页：活动项每次 50 条、完成项每次 5 条。搜索匹配标题、ID、项目、创建者和标签，不读取全部 Markdown；CLI 原有的任务全文检索保持独立。最后更新时间来自持久化 `updated_at`，外部文档编辑由后台分批投影。
负责人、子任务层级及开始/截止日期尚无对应数据，不提供占位控件。收件箱以筛选菜单中的“查看归档”切换归档范围。

### Web 工作流

工作板默认显示未完成任务，可切换待验收、等待中和全部；全部视图的已完成组默认折叠。
列表、看板、泳道和表格共享筛选及选择状态；全选覆盖全部匹配结果，包含尚未加载的页，允许逐项排除。服务端按筛选集合成员返回 `selection_revision`，集合成员变化时清空旧选择并提示；排序、布局或内容未变的刷新保留选择。
批量修改项目、优先级或归档前最多预览 100 项，并重新取得 ETag 检测并发冲突；逐项报告结果，不自动重试失败项。
只有明确导出 CSV 时才遍历完整选择范围。SSE 健康时查询每 60 秒兜底，断线后列表每 15 秒、详情每 5 秒重验。
详情顶部显示最近进展和计划中的下一步，项目、优先级可就地编辑，支持展开阅读。

默认打开 ToDo，主导航只展示 ToDo、知识库和“更多”，设置与搜索保留为工具入口。“更多”包含收件箱、Agent 会话、统计和消息，当前进入的辅助模块会高亮“更多”。

任务详情的“关联知识”支持搜索文档、添加与取消关联；知识文档底部显示关联任务，可关联已有任务或填写标题与说明创建任务。新建任务与知识关联在同一事务中保存，沿用创建幂等键。关联复用 `todo_links`，以 `/knowledge?document=<id>` 保存，不绑定本机端口、文档标题或集合；知识反向查询每页 50 条，包含只读归档任务，不读取任务正文或 Agent 会话。只读权限隐藏写入入口，服务端独立验证人工 Web 写入与文档身份。

Agent 页默认看主力来源 Codex，可切换全部或其他辅助来源，
并按项目、时间筛选，默认按显式父子关系聚合后分页；子会话展开时单独分页，
父会话不在筛选范围内的子会话仍可见。任务的会话关联数量与 Hook 观测到的实际活动分别显示。
统一搜索默认限定当前工作区及项目/集合等上下文，可切换全部内容。收件箱空状态提供清除筛选、
管理来源和查看归档入口；AI Day 收敛为统计页的“徽章日历”，按月展示每日徽章，点选日期查看名称与简短说明；旧 `/ai-day` 地址自动转入统计。
