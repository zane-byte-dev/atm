# ATM Menu

macOS 13.4+ 的轻量菜单栏伴随 App。它只使用 AppKit、SwiftUI、UserNotifications、ServiceManagement 和本机 HTTP API；不链接 SQLite、语音引擎，也不在 App 内扫描会话、同步数据或运行采集任务。

```sh
app/menubar/Scripts/build-app.sh
swift test --package-path app/menubar --scratch-path app/menubar/.build --cache-path app/menubar/.cache --disable-sandbox
```

点击状态项直接打开普通 macOS 菜单。状态栏标题在任务和高用量额度摘要后追加紧凑的今日 Token，例如 `任务 5 · Codex 92% · 387M`；加载失败时不显示错误占位。菜单顶部显示 Go 服务状态和只读的“今日 Token”，随后是有界的当前任务与 Agent 额度摘要；底部提供打开 ATM、新增任务、同步并刷新、设置和退出。任务与额度行通过一次性 ticket 打开对应 Web 页面。全局快捷键默认是 ⌥⌘A，用于直接打开 ATM Web 工作区。

App 每 10 秒调用 `POST /api/v1/control/companion`，读取 `snapshot`、通知 `feed`、`todos`、`quota` 和 `today_usage`。`today_usage` 的 `total_tokens` 为零时明确显示 0，读取失败时显示“暂不可用”；迁移期间也能读取旧 runtime 的 `quick.usage.ranges.today`。菜单中的“同步并刷新”只要求 Go runtime 排队一次同步，实际同步仍由 Go 后台完成。

通知开关启用且系统已授权时，App 才领取原生通知租约。显示或撤回后通过 companion ack 推进游标；关闭通知会把全局系统通知偏好持久化到 Go 服务，同时保留菜单内的只读通知 feed，并阻止 Go fallback 弹出系统横幅。重新开启时从当前游标开始，不补播关闭期间的旧提醒。通知点击按任务、Agent、收集等类型打开 Web，不包含原生 Guard 审批界面。

独立偏好域是 `dev.zanebyte.atm.companion`。默认读取 `~/.atm/runtime/server.json` 与 `control.token`；可从设置选择其他数据目录。客户端只接受 `schema_version=1`、匹配所选目录的实例记录和显式 `127.0.0.1` origin，并拒绝 HTTP 重定向。设置子菜单保留原生通知、声音、全局快捷键、登录启动、数据目录和旧偏好白名单导入。

构建只写入仓库内 `app/menubar/dist`，不会自动安装、启动或注册登录项。新包不引用 `app/macos`；声音资源附带原许可证。
