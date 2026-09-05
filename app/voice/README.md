# VoxCaret 声标

独立 macOS 13.4+ 语音输入 App：长按右 Command 或备用快捷键说话，松开后文字落在当前光标处。只依赖 Apple 框架和固定版本的 sherpa-onnx-spm，不依赖其他产品服务或运行时。
中文系统显示名为“声标”，其他系统显示名为 `VoxCaret`；产物和可执行文件统一使用稳定的英文名。

```sh
app/voice/Scripts/build-app.sh
app/voice/Scripts/package-app.sh
open "app/voice/dist/VoxCaret.app"
swift test --package-path app/voice --scratch-path app/voice/.build --cache-path app/voice/.cache --disable-sandbox
```

`build-app.sh` 产生仓库内的 `.app`；`package-app.sh` 另外生成保留签名和扩展属性的 ZIP 分发包。
两个脚本都不安装、不启动、不改系统权限。构建启用 Hardened Runtime，并携带
`com.apple.security.device.audio-input` 麦克风 entitlement；同时保留 Info.plist 中的麦克风与语音识别
用途说明。脚本优先使用钥匙串中的 Apple Development 身份，没有时回退到 ad hoc；可用
`VOXCARET_CODESIGN_IDENTITY` 显式指定。安装到稳定路径后再授权，以免频繁改变签名路径影响系统记录。
正式外部分发仍需 Developer ID Application 签名和 Apple 公证。

偏好域 `dev.zanebyte.voxcaret`；模型目录 `~/Library/Application Support/VoxCaret/VoiceModels/SenseVoiceSmall-int8-2024-07-17/`。日志使用该 bundle ID 的 macOS unified log，不记录转写正文。最近文本只在内存，退出即消失。

首次设置页可从 ATM Voice 独立版、旧 ATM 正式版或 Dev 版导入七项语音偏好及完整模型。每个文件先复制到目标卷上的临时目录，再按 SHA256 比较源/副本，完整后原子重命名。导入不删除旧数据，也不复制转写历史或其他业务数据；已存在的完整 VoxCaret 模型会保留。缺失模型可通过设置下载，下载归档有固定校验值。VoxCaret 会优先只读复用本机已有的兼容版 SenseVoice Small，不重复占用约 240 MB；闪电说的模型虽然词表相同，但其 ONNX 缺少 sherpa-onnx 所需元数据，因此不会冒险直接加载或修改。

SenseVoice 本地模型未就绪时会回退 Apple Speech。界面显示实际引擎；Apple 本机识别开关关闭时可能通过网络识别，打开后不支持的语言会报错，不静默改为网络识别。

“实时输入（Apple Speech）”默认开启，并固定使用 Apple Speech。partial result 会经过短暂合并后直接预览到录音开始时的输入位置，新结果会替换上一版预览而不是重复追加；Apple Speech 因停顿提前结束一个识别段时，会保留已有文字并自动续开下一段。松手时用最终清理结果校正，Esc 或识别失败会撤回已经写入的预览。目标焦点变化或缺少辅助功能权限时停止实时写入并退回最终一次性注入。关闭实时输入后才按首选引擎转写；SenseVoice 是非流式本地模型，因此在松手后一次写入。

麦克风、Apple Speech 和辅助功能权限属于新 bundle，必须由用户在 macOS 弹窗/系统设置确认，无法迁移。长按右 Command 默认开启，短按不动作，约 0.2 秒后开始录音；它依赖辅助功能权限，同时运行闪电说时应关闭其中一个应用的该入口。备用快捷键冲突时 VoxCaret 会显示冲突而不抢占。辅助功能未授权时保留转写到剪贴板，供手动粘贴。

已覆盖模型完整性、导入边界、快捷键偏好、文本整理、授权超时、取消后不注入、异步粘贴按键释放的自动测试。仍需在实际目标 App 中由用户验收按住/松开、短按、Esc、Space/前台切换、拒绝权限及粘贴恢复；构建/单测不代替实际权限和麦克风验收。旧代码保留在 `app/macos` 作为历史实现参考，新产品构建不引用它，当前 Go 也不提供旧主工作区的运行接口。
