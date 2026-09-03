# ATM Web 开发

当前 Web 提供七个主工作区。页面按需加载，均接入现有本机数据；旧数据库的只读预览覆盖所有工作区。

“设置 → 外观”提供极简黑白、石墨雾蓝、鼠尾草绿、暖砂和午夜五种主题，默认极简黑白。选择即时应用到七个工作区，
通过当前站点的 `localStorage` 保存，刷新后保留并同步同源标签页；存储不可用时仍可在本次浏览期间切换。
外观设置独立于后端写入能力，只读预览也可使用。主题变量集中在 `src/themes.css`，包含背景、文字、
选中态、状态色、弹窗与图表；浏览器的原生控件随主题切换明暗。
页头字号、说明文字、页面留白、指标条、卡片圆角和按钮使用 `src/style.css` 中的共享尺寸变量。
桌面下收集、会话和知识的列表与详情分别滚动；
窄窗口使用列表与详情切换，详情提供返回入口。升级说明默认折叠，展开后仍可查看完整操作步骤。

| 工作区 | 已接入的操作                                                                                         |
| ------ | ---------------------------------------------------------------------------------------------------- |
| 任务   | 搜索、状态与项目筛选、分页、详情、进展、计划、新建、编辑、开始、完成、归档和恢复                     |
| 收集   | 来源与条目、未读/已读/归档筛选、结论与本地消息历史、标记已读、归档恢复、来源启用与静音               |
| Agent  | 按 Agent/项目/日期筛选会话、全文检索、分页对话与工具计数、索引新鲜度                                 |
| 知识   | 集合与文档搜索、Markdown 详情、新建集合/文档、从已有文档创建副本；共享记忆检索、新建与原子修订       |
| 统计   | 日期范围、Agent 筛选、用量趋势、项目/模型/技能等拆分，以及标注观测时间的缓存额度                     |
| AI Day | 已生成的每日结果、历史、徽章、来源覆盖与衍生事件账本                                                 |
| 设置   | 五种本地主题、个人昵称、模型与连接器配置摘要、凭据是否配置、后台与同步状态；服务端偏好只开放昵称修改 |

任务规则由 Go Work service 处理；浏览器负责界面、草稿与并发冲突展示。文件型知识文档目前通过创建
副本继续编辑；原地修改需要先补跨进程版本保护。API 不接受任意导入路径、命令、凭据或 Guard 决策。

首版草稿使用各标签页独立的 `sessionStorage`，刷新和切页后可恢复；另一标签页保存或提交不会删除
本页草稿。关闭标签页后的恢复不作保证。

任务页面前台每 5 秒轮询并在重新聚焦时刷新，其他工作区使用独立查询和更长刷新间隔。
已有附件通过授权接口读取，暂不支持浏览器上传。页面读取不会同步会话、执行外部采集、刷新实时账单、
重新生成 AI Day 或调用模型。

SSE、Go 同源的 Vite HMR、后台同步/收集/hook 接管和原生应用拆分还在
[迁移方案](../../docs/design/local-web-runtime.md)中。需要现有后台能力时继续运行 macOS App。
Workspace 模式的任务操作保留文档同步与 `on_done`，桌面通知仍由旧 App 展示，避免双重通知。

## 构建完整程序

需要 Go 1.25+、Node.js 24 和 npm。从仓库根目录执行：

```sh
make build
bin/atm serve --open
```

`make build` 串行执行 `npm ci`、TypeScript 检查、Vite 生产构建和带 `webui` 标签的 Go 构建。
页面嵌入二进制，运行机器无需 Node、npm 或源码目录。`make dist` 和 GoReleaser 使用相同的嵌入方式。

默认 `go build`、`go install` 和 `make build-cli` 不带 `webui`，无需前端依赖或 `dist` 也可编译。
这类产物可以使用 CLI；启动新的 Web 实例时会提示安装完整构建。

`make build` 和 `make build-cli` 默认都输出 `bin/atm`。若该路径正在被日常 CLI 或 macOS App 使用，
开发时指定独立名称，使用仓库内的 `bin` 目录，避免 macOS 对临时目录中可执行文件的限制：

```sh
NPM_CONFIG_CACHE=/private/tmp/atm-web-npm-cache make build APP=atm-web
bin/atm-web serve --data-dir /private/tmp/atm-web-data --port 0 --open
```

`NPM_CONFIG_CACHE` 可选；这里使用单独缓存，避免默认 npm 缓存的权限问题。`--data-dir` 只适用于
`serve` 及其子命令，不改 HOME，也不启动同步或收集。写入验收使用空目录或脱敏副本，切勿把临时测试
实例指向日用库。

## 页面修改与验证

```sh
npm run check --prefix app/web
npm run test --prefix app/web
npm run build --prefix app/web
go build -tags webui -o bin/atm-web ./cmd/atm
```

修改后重新构建、停止旧实例，再启动新二进制。生产资源在编译时嵌入，运行中的程序不会自动加载新
`dist`。同一数据目录只运行一个实例；换了可执行文件也应先停止旧实例：

```sh
bin/atm-web serve stop --data-dir /private/tmp/atm-web-data
bin/atm-web serve --data-dir /private/tmp/atm-web-data --port 0 --open
```

浏览器第一次连接和实例重启后都通过 `serve --open` 建立会话。直接打开地址可能显示连接提示。
`npm run dev` 只启动 Vite；当前尚无与 Go 的同源开发代理，不能据此验收真实 API 或鉴权。完整端到端
验证应使用上面的嵌入资源构建，不为开发临时放开 CORS。

应验证真实任务闭环、CLI 并行修改、编辑冲突、草稿恢复、创建重试、归档恢复和服务重启。
生命周期写入只在隔离数据目录进行。页面不提供 Guard 授权或任意本机文件读取入口。

## 升级数据

现有 schema 54 数据库首次通过 `serve --open` 打开时只读，不会自动迁移。准备启用写入时：

1. 执行 `bin/atm-web serve stop`，退出旧 macOS App，并暂停其他 ATM 写入。`serve stop` 不会自动退出 App。
2. 将日用 CLI 与 macOS App 所配置的 CLI 统一到同一份新二进制。
3. 执行 `bin/atm-web serve migrate`。命令先把升级前备份写入数据目录的 `backups/`，备份成功后才升级
   至 schema 55；备份失败则不迁移。保存输出的备份路径。
4. 执行 `bin/atm-web serve --open`，再恢复 macOS App 的后台功能。

以上命令默认使用日用数据目录；隔离验收时每条都应加相同的 `--data-dir /private/tmp/atm-web-data`。
新建空工作区无需迁移。

创建幂等记录使用 schema 55。旧 schema 54 二进制不能直接打开升级后的数据库；要回退，须停止所有
写入、另存升级后的数据，再恢复升级前的备份。前端不执行 schema 降级，也不自动丢弃备份之后的修改。
