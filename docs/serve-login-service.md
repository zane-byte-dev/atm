# macOS 登录服务

完整构建的 ATM 可以作为当前用户的 LaunchAgent 运行。它直接启动当前命令使用的完整 CLI，不复制二进制，也不需要管理员权限。

先审阅生成的配置：

```sh
/absolute/path/to/atm serve install --data-dir ~/.atm --port 47321 --print
```

`--dry-run` 与 `--print` 相同，只输出 plist；不会创建目录、安装服务或调用 `launchctl`。仅 CLI 的构建不含 Web 页面，不能安装为 Web 服务。

实际安装：

```sh
/absolute/path/to/atm serve stop --data-dir ~/.atm
/absolute/path/to/atm serve install --data-dir ~/.atm --port 47321
/absolute/path/to/atm serve --data-dir ~/.atm --open
```

配置保存到 `~/Library/LaunchAgents/com.atm.workspace.<数据目录摘要>.plist`，每个数据目录使用独立标签。启动参数是 `serve --background --data-dir <规范绝对路径> --port 47321`。它在登录后启动，退出后由 launchd 以至少 10 秒间隔重启。PATH 保留安装时的绝对目录，并补充常见 CLI 安装目录；没有从终端复制令牌或其他环境变量。

plist 与 `<data-dir>/runtime/serve.stdout.log`、`serve.stderr.log` 的权限均为 `0600`。安装器拒绝符号链接、非普通文件及不属于 ATM 的同名 LaunchAgent。重复安装相同配置不会重启已有进程；更改路径或端口后重新安装会重新加载这个服务。升级同一路径下的 CLI 后，先 `serve stop` 再 `serve install` 可启用新构建。

`serve stop` 会先卸载当前 launchd job，避免立即重启；launchd 为 HTTP 请求收尾与后台任务取消保留 45 秒退出时间。保留的 plist 会在下次登录时重新启用。再次 `serve install` 可以立即恢复运行。已有未托管的 Web 服务需要先停止，然后才能安装，安装器不会抢占其进程。

彻底撤销自启动：

```sh
/absolute/path/to/atm serve uninstall --data-dir ~/.atm --dry-run
/absolute/path/to/atm serve uninstall --data-dir ~/.atm
```

卸载只停止对应服务并删除经过验证的 plist。数据库、文档、日志，以及 `runtime/presence-owner.json` 中的 Go 所有权记录都会保留；旧 macOS App 不会因此自动恢复后台工作或抢占 Agent Hook 接收器。二进制移动后，需要从新位置重新执行安装命令。

安装不会迁移数据库，也不会修复未配置的连接器或登录态；旧版本数据库应先通过 `atm serve migrate` 备份并升级。
