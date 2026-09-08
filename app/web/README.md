# ATM Web 开发

ATM 的主界面由浏览器承载，同一个 Go 二进制提供 CLI、Web API、嵌入页面和后台服务。七个工作区按需加载，
无需 Swift 主窗口。当前代码已接入以下范围；真实日用库升级、登录服务安装和原生系统权限验收由上线步骤单独确认，
不能把构建或隔离测试通过视为已经切换日用环境。

| 工作区 | 已实现的操作 |
| --- | --- |
| 工作板（任务） | 列表/看板/表格/泳道、组合筛选、排序与属性显示、浏览器保存视图、弹窗详情与文档、新建编辑、生命周期、计划、进展、依赖、等待条件、图片上传及明确发起 AI 整理 |
| 收件箱（收集） | 待处理结果与详情、来源筛选、已读/归档，以及二级入口中的来源管理、处理记录、本地消息历史、手动采集及重新处理 |
| Agent | 会话筛选检索、分页对话、工具计数、任务关联、索引新鲜度、Hook 实时状态汇总与手动同步 |
| 知识 | 集合/文档搜索与新建、本地文档原地编辑及版本冲突、外部文档副本、共享记忆检索/新建/修订 |
| 统计 | 日期、Agent/模型/项目筛选，趋势与明细，缓存额度及手动刷新已配置的额度来源 |
| AI Day | 每日结果、历史、徽章、覆盖与衍生事件账本，以及明确生成今天/重建所选日期 |
| 设置 | 五种主题、旧版界面偏好导入、业务偏好、模型地址与名称、只写凭证管理、运行状态和后台执行记录 |

“设置 → 外观”提供极简黑白、石墨雾蓝、鼠尾草绿、暖砂和午夜五种主题，默认极简黑白。
选择通过当前站点的 `localStorage` 保存，刷新后保留并同步同源标签页；存储不可用时仍可在本次浏览期间切换。
外观与偏好导入独立于后端写入能力，只读预览也可使用。主题变量集中在 `src/themes.css`，共同尺寸在 `src/style.css`；
各页统一页头、留白、卡片、指标和按钮。收集、会话和知识的列表/详情分别滚动，窄窗口提供返回入口。

新建任务会自动记住当前填写的标题、说明、项目和优先级，再次打开时自动填入，创建成功后清除。
多窗口以最后一次填写为准，较早的创建响应不会删除较新的输入；创建重试仍使用原来的幂等身份。
编辑已有任务不保存或恢复浏览器内容，重新打开时读取服务端版本；并发编辑冲突仍需核对合并。
知识编辑器的保存机制独立。浏览器存储失败会显示提示。

图片支持文件选择、拖放和粘贴，限 PNG/JPEG/GIF、单张 10 MB、每任务 10 张。上传逐张附带当前 ETag，
服务端检查实际图片格式、尺寸/数量和任务版本；读取通过授权附件接口，不接受任意本机路径。
本地知识文档使用跨进程锁和 ETag 原地保存；有外部来源的文档创建副本，持续关联源文件的导入仍走 CLI。

页面通过 SSE 订阅当前路由相关域；服务内提交主动失效，外部 CLI/文件变化在有订阅时每 2 秒检查域版本与文件内容指纹。
事件只带变更域，不推会话正文。重连支持有界重放/reset，后台标签页断开，回到前台重新查询；轮询和重新聚焦仍作兜底。

`serve` 接管会话同步、自动采集、AI Day、Agent Hook 和通知路由。
后台约每 5 分钟同步、每 7 分钟更新 AI Day，采集按已启用来源的到期规则执行；Hook 即时更新并每 8 秒回补状态。
关闭页面不停止这些工作。手动操作通过白名单 `jobs.run/list/show/cancel` 执行，包括 `session.sync`、`collect.run`、
`collect.reprocess`、`day.rebuild`、`quota.refresh` 和 `todo.refine`。执行记录落 SQLite，有界队列、取消、超时和重启中断状态；
读取页面不会自行提交手动作业。模型配置和凭证管理不会试调模型；凭证不回传页面、不写入浏览器存储。

CLI 和 Web 复用业务服务、同步/采集执行锁及模型记账；Go 负责通知事实、去重和渠道路由。
可选 [ATM Menu](../menubar/README.md) 负责原生显示，独立 [ATM Voice](../voice/README.md) 负责全局语音。
旧 Swift 主工作区已移除，源码及性能改动保存在 [Git 历史](https://github.com/zane-byte-dev/atm/tree/f96ed66f35dbd099de5ab16dbbea008c60381e27/app/macos)。
旧界面偏好导出工具独立保留在 [`scripts/export-web-preferences.swift`](../../scripts/export-web-preferences.swift)。
API 不提供任意命令、配置键、Guard 审批或任意文件读取入口。

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

## 数据基线与登录服务

当前构建创建并只接受 schema v57。历史迁移梯子和旧 schema 的只读 Web 模式已经删除：旧库不会被
猜测性打开，也不能由当前二进制跨多个历史版本升级。需要保留旧库时，先用仍支持它的历史版本升级，
或按错误提示备份不可重建数据并重建当前数据库。`atm serve migrate` 只为以后明确注册的单阶迁移保留；
当前 v57 基线执行它只会报告无需迁移。

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

## 迁移旧版界面偏好

从仓库根目录导出正式版或开发版偏好：

```sh
mkdir -p bin
swift scripts/export-web-preferences.swift > bin/atm-native-preferences.json
# 旧 ATM Dev 使用 --dev
```

在“设置 → 外观 → 迁移旧版界面偏好”选择该 JSON，预览勾选后确认。白名单只有
`knowledge_collection_order`、`collection_source_order`、`usage_filter_model`、`usage_filter_client`、
`usage_filter_project`。文件最多 2 MB，排序最多 1,000 项；未知字段、错误版本和无效格式会被拒绝。
文件仅在浏览器解析，不上传到 Go；排序中已删除的 ID 忽略，新条目追加在后。

用量筛选复用已有模型/项目时间桶。选中项目时项目汇总和趋势优先，不把独立的模型与项目记录伪造成
联合统计。偏好保存在当前浏览器，其他浏览器需要各自导入；主题、业务配置和凭证不在这个文件中。
窗口尺寸和原生面板折叠采用新网页布局；语音的七项偏好和模型、菜单栏的通知/音效/呼出快捷键分别在独立 App 导入。

工作板四种模式共用筛选与排序，切换模式不会清除条件。泳道按项目分行、状态分列；列表可按状态、项目分组或不分组。保存视图包含布局、筛选、排序、密度和属性显示，存于当前浏览器 localStorage；不保存新建任务信号。任务数据从本地 API 分页读取完成后统一筛选，不只筛选首屏。负责人、子任务层级及开始/截止日期尚无对应数据，不提供占位控件。收件箱以筛选菜单中的“查看归档”切换归档范围。

### Web 工作流

工作板默认显示未完成任务，可切换待验收、等待中和全部；全部视图的已完成组默认折叠。
列表、看板、泳道和表格共享筛选及选择状态，支持预览后批量修改项目、优先级或归档。
属性更新以预览时的 ETag 检测并发冲突，批量操作逐项报告结果，不自动重试失败项。
详情顶部显示最近进展和计划中的下一步，项目、优先级可就地编辑，支持展开阅读。

Agent 页可按项目、时间和 Agent 筛选，默认按显式父子关系聚合后分页；子会话展开时单独分页，
父会话不在筛选范围内的子会话仍可见。任务的会话关联数量与 Hook 观测到的实际活动分别显示。
统一搜索默认限定当前工作区及项目/集合等上下文，可切换全部内容。收件箱空状态提供清除筛选、
管理来源和查看归档入口；AI Day 收敛为统计页的“徽章日历”，按月展示每日徽章，点选日期查看名称与简短说明；旧 `/ai-day` 地址自动转入统计。
