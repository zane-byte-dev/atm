# ATM Voice

独立 macOS 13.4+ 语音输入 App。只依赖 Apple 框架和固定版本的 sherpa-onnx-spm；不连接 ATM 服务，不读取 SQLite、会话、任务或 runtime 文件。快捷键管理器只有语音输入和录音期间的 Esc 取消。

```sh
app/voice/Scripts/build-app.sh
open "app/voice/dist/ATM Voice.app"
swift test --package-path app/voice --scratch-path app/voice/.build --cache-path app/voice/.cache --disable-sandbox
```

构建仅产生仓库内的 `.app`，不安装、不启动、不改系统权限。脚本优先使用钥匙串中的 Apple Development
身份，没有时回退到 ad hoc；可用 `ATM_CODESIGN_IDENTITY` 显式指定。安装到稳定路径后再授权，以免
频繁改变签名路径影响系统记录。正式分发仍需 Developer ID 签名和公证。

偏好域 `dev.zanebyte.atm.voice`；模型目录 `~/Library/Application Support/ATM Voice/VoiceModels/SenseVoiceSmall-int8-2024-07-17/`。日志使用该 bundle ID 的 macOS unified log，不记录转写正文。最近文本只在内存，退出即消失。

首次设置页可选择正式旧 ATM 或 ATM Dev，点击导入七项语音偏好并复制已有完整模型。每个文件先复制到目标卷上的临时目录，再按 SHA256 比较源/副本，完整后原子重命名。导入不删除旧数据，不复制转写历史或任何 ATM 业务数据；已存在的完整独立模型保留。缺失模型可通过设置下载，下载归档有固定校验值。

SenseVoice 本地模型未就绪时会回退 Apple Speech。界面显示实际引擎；Apple 本机识别开关关闭时可能通过网络识别，打开后不支持的语言会报错，不静默改为网络识别。

麦克风、Apple Speech 和辅助功能权限属于新 bundle，必须由用户在 macOS 弹窗/系统设置确认，无法迁移。旧 App 的语音热键应先关闭；若它仍占用快捷键，新 App 会显示冲突，不抢占。辅助功能未授权时保留转写到剪贴板，供手动粘贴。

已覆盖模型完整性、导入边界、快捷键偏好、文本整理、授权超时、取消后不注入、异步粘贴按键释放的自动测试。仍需在实际目标 App 中由用户验收按住/松开、短按、Esc、Space/前台切换、拒绝权限及粘贴恢复；构建/单测不代替实际权限和麦克风验收。旧代码保留在 `app/macos` 作为回退来源，新产品构建不引用它。
