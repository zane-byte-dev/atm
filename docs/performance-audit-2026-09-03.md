# ATM macOS 客户端性能排查

核验时间：2026-09-03。源码基线：`c87f3cf`，排查开始时工作区干净。本文保留首次只读排查时的现象、源码行号和实测基线；其中“当前”“本轮”均指首次排查阶段。随后根据用户要求实施了修复，见 [修复与回归记录](performance-fixes-2026-09-03.md)。

## 结论与证据边界

当前卡顿有两条相互放大的路径：任务切换时的主线程重复计算，以及与当前页面无关的全量后台刷新。首先应修复任务列表派生数据和 Store 更新范围，同时拆开 work / stats 的刷新。缩短动画可以改善手感，但不能替代这两项。

扫描了客户端全部 84 个 Swift 源文件中的加载、刷新、状态观察、视图身份、同步 I/O 和文本布局入口；对任务、收集、Agent、知识、搜索、统计、AI Day、设置、菜单栏、图片和语音相关路径做了定向调用链检查，并核对了对应 Go dashboard 服务。并非每一个页面都进行了交互性能采样。

运行时验证包括：当前 ATM 进程识别、5 秒空闲采样、20 秒任务切换采样、已有任务间的浏览切换，以及 work / stats / full 各两次只读 IPC 耗时测量。浏览结束已回到原任务。未建立 Release 对照，未测量精确点击到首帧延迟或长文/图片极端场景，未与具体 Web 实现做同机对比。

“实测”表示本机观测；“静态确认”表示代码路径存在，其具体耗时仍需按场景测量；“条件性风险”仅在相应数据规模或操作下成立。采样调用栈的出现次数不能当成单次点击耗时。

## 本机实测

### 当前运行的是 Debug 客户端

运行路径为 `app/macos/.build/dev-app/ATM Dev.app/Contents/MacOS/ATMMenuBarApp`。开发脚本使用 `.build/debug` 和未指定 Release 的 `swift build`；构建描述中有 `-Onone`、`-enable-testing`、`-g`。这是性能放大因素，不足以证明换成 Release 就能解决全部问题。

来源：[开发脚本](/Users/mj/mox/atm/app/macos/Scripts/run-dev-app.sh:5)、[Release 构建脚本](/Users/mj/mox/atm/app/macos/Scripts/build-app.sh:12)。运行包经签名后与未打包二进制哈希不同，本轮没有把它们宣称为逐字节相同。

### 后台全量快照约 9.4 秒

调用当前 `/usr/local/bin/atm _ipc dashboard.snapshot`，stdin 分别为 `{"sections":["work"]}`、`{"sections":["stats"]}`、`{}`。不执行 sync，所有调用退出码为 0。

| 数据范围 | 第一次 | 第二次 | stdout 大小 |
| --- | ---: | ---: | ---: |
| work | 0.123 s | 0.116 s | 261,408 bytes |
| stats | 10.037 s | 9.640 s | 2,316,418 bytes |
| full | 9.375 s | 9.508 s | 2,576,402 / 2,576,422 bytes |

当时 work 返回 246 个任务；界面另外显示 85 个归档任务。一次进程快照观察到 App 自己的 `atm _ipc dashboard.snapshot` 子进程 CPU 约 298%。这是单次进程统计，不代表持续 CPU 均值。测量期间原 App 仍运行，结果不是隔离基准，也不代表生产机器普遍耗时。

这 9–10 秒是后台 IPC 耗时，**不是每次切换任务都等待 9–10 秒**。已有任务标题和描述来自内存；但全量查询增加后台资源占用，刷新结果又会触发界面更新。

### 任务切换采样发现全列表排序

5 秒空闲采样中主线程大多等待事件，没有看到持续阻塞。20 秒交互采样中出现以下主线程调用链：

```text
DesktopTasksView.body
  → taskList / taskColumn / groups
  → ATMTaskQuery.groups
  → sortedByCompletionDescending
  → completionSortKey
```

同一采样也出现 `ATMMarkdownContentView.markdownText → ATMMarkdown.render → protectBareLinks`。样本中是普通短文本任务，不能据此推导长文、图片或完整会话的性能上限。

原始临时证据：[空闲采样](/tmp/atm-perf-idle.sample.txt)、[切换采样](/tmp/atm-perf-switch.sample.txt)、[IPC 测量结果](/tmp/atm-perf-probe-results.json)、[测量脚本](/tmp/atm_perf_probe.py)。临时目录可能被系统清理；上表和调用链为本报告保留的摘要。

## 问题清单

P1 为应优先修复的高频或全局问题；P2 为后续应处理的问题；P3 为特定场景或长期使用风险。优先级不等同于已测得的耗时排序。

### F01 · P1 · 任务刷新被全量统计和配额绑定【实测 + 静态确认】

`start()` 每 60 秒调用完整 `refresh()`；后者不给 dashboard 指定 sections，并等待 dashboard、quota、archive 全部结束才应用结果。后端统计固定生成全部 7 个时间范围，每个范围提交模型、技能、项目、会话列表、速度 5 组查询，另有 7 组通用统计查询。不是只取当前统计页正在看的范围。

冷启动已经用 `primeWork()` 优先显示任务，但普通刷新与部分操作后的刷新没有沿用该轻量路径。启动还先完整 refresh，再排队 sync + 完整 refresh。

影响：看任务也周期性承担统计聚合；任务刷新受最慢支路影响；重复启动聚合增加负载。建议 work 独立更新，stats 按需按范围取数、共享有版本的缓存，quota 独立落地；合并启动刷新，保留任务乐观更新及旧响应保护。

来源：[刷新调度](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:1089)、[等待全部请求](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:2397)、[统计展开](/Users/mj/mox/atm/internal/dashboard/service.go:257)、[每范围查询](/Users/mj/mox/atm/internal/dashboard/service.go:409)。

### F02 · P1 · 选择一个任务会重新分组、重复排序所有完成任务【采样命中 + 静态确认】

`DesktopTasksView.groups` 是计算属性，任务选择和 Store 更新会再次进入它。`groups(from:)` 先排序全部 done，再分成最近完成和完成历史，之后又对两个分组排序。折叠只控制视图显示，不跳过这些派生计算。排序比较器还反复构造完成日期和数字 ID 排序键。

影响：历史任务越多，点击当前任务越容易带出无关工作。建议在任务集合版本或日期边界变化时计算分组；预计算排序键，一次排序后分桶；选择态更新只影响旧、新选中行和详情。不能把“折叠了”当成“没有计算”。

来源：[重复排序](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:273)、[分组计算属性](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:1197)。

### F03 · P1 · 实时状态通过全局 Store 使任务等无关界面失效【静态确认】

任务、收集、知识、搜索及根视图都观察 `ATMDataStore`，它包含大量不相关的 `@Published` 字段。Agent 通知器从启动开始持续持有实时轮询；即使离开 Agent 页也不会停止。轮询通常为 3 秒，近期有 hook 时按 8 秒策略兜底。

`reapplyAttentionOverlay()` 虽然比较前后 liveStatus，但 `ATMLiveStatus` 的合成 Equatable 包含每次更新的 `time`，会话还包含变化的 `ageSeconds`，因此该相等判断不能隔离大多数轮询刷新。它更适合过滤同一快照上重复的 hook overlay，而不是证明界面数据未变。

建议按 tasks / presence / collection / knowledge / settings 拆分可观察状态，或增加页面、列表派生数据的相等边界；时钟和年龄展示局部更新。保持后台通知能力，不用简单关轮询来换流畅度。

来源：[共享 Store](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:842)、[overlay 更新](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:1240)、[状态时间字段](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/SessionModels.swift:450)、[常驻轮询](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMAgentAttentionNotifier.swift:59)。

### F04 · P2 · 详情打开提前加载未查看的内容，快速切换后请求继续运行【静态确认】

任务详情 onAppear 加载 sessions、progress、advice。progress 和 sessions 仅阻止同 ID 的并发重复请求，已有结果不能阻止重新读取；advice 已有 300 秒节流，不应一并描述为无缓存。读取通过 Store 内部 `Task` 启动，没有随选择离开取消的任务句柄。连续浏览多个任务时，旧请求仍完成并发布全局状态。

建议按页签和可见区域加载，按任务版本缓存并后台校验；明确请求所有权，取消已经无消费者的读取。若需要预取，限制并发并避免预取结果触发整个工作区更新。

来源：[详情加载入口](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:1829)、[progress / sessions](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:2861)、[建议节流](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:1501)。

### F05 · P2 · 通用切换组件把动画与视图销毁绑定【静态确认】

`atmAnimatedSwap` 内部无条件 `.id(identity)`；任务、Agent、知识详情和页面切换均使用它，时长为 180–220 ms。任务详情自身又 `.id(todo.id)`。切换身份使 SwiftUI 替换内容及其局部状态，并非单纯调整透明度。Agent 的最新动态还把整段 text 当作身份。

建议把视觉过渡和状态生命周期分开；详情壳保持稳定，按对象管理状态，选择反馈立即显示，内容过渡缩短或取消。某些重置是正确语义，例如切换任务不能沿用另一任务的编辑草稿，不能直接全局删除 `.id` 而不补状态规则。

来源：[通用实现](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMTheme.swift:168)、[任务详情](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:1245)、[Agent 动态](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopAgentsView.swift:792)。

### F06 · P2 · Markdown 重复解析发生在视图构建路径【采样命中 + 静态确认】

`ATMMarkdownContentView.init` 每次构建都分块；正文、标题、列表、表格单元格在 body 中调用 `ATMMarkdown.render`。每次 render 都创建链接检测器并扫描文本；对每个链接再从头判断代码区间，链接很多时会进一步放大工作量。进展列表也直接在行构建中解析文本。

建议缓存按原文版本生成的分块和富文本，复用链接检测器，在需要时后台解析；字体/主题变化与文本解析分离。对超长内容限制首批渲染量。尚未测量长 Markdown 的绝对耗时。

来源：[分块与富文本](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMMarkdownContentView.swift:17)、[链接保护](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMMarkdown.swift:357)、[进展正文](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/TodoProgressView.swift:104)。

### F07 · P2 · 分组内及完整会话未按行虚拟化【静态确认；大数据场景】

通用导航外层是 LazyVStack，但 `ATMNavigatorGroup` 内层是普通 VStack，一个展开的大分组仍会构建整组内容。任务、Agent、收集共用该结构。完整会话和时序视图也使用 VStack + ForEach 构建全部轮次/事件；每轮内部再构建 Markdown。展开全部任务进展同样走普通 VStack。

建议虚拟化到“行/轮次”而不是只到“组”，必要时分页；长会话首批展示可见轮次，保留全文检索和跳转能力。旧的纯文本弹窗已经使用 NSTextView，不应把这一发现误套到它。

来源：[分组容器](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDesignSystem.swift:130)、[会话全轮次](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopSessionTranscriptView.swift:109)、[时序](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopSessionTranscriptView.swift:195)。

### F08 · P2 · 图片读盘与粘贴转码在 UI 路径同步执行【静态确认；图片场景】

任务详情和新建表单在视图构建中 `NSImage(contentsOf...)`，没有独立缩略图缓存；粘贴处理同步获取 TIFF、转换 PNG 并原子写盘。实际图片解码时机受 AppKit 影响，本轮没有把所有解码都断言为主线程，但同步读盘和转码路径明确存在。

建议异步生成下采样缩略图，按路径/修改时间/显示尺寸缓存；UI 仅接收结果。粘贴先确认图像类型，再后台转码和写临时文件，用稳定占位保持布局。

来源：[任务图片](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:2151)、[表单图片](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:2862)、[粘贴转码](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:2948)。

### F09 · P2 · 跨工作区返回会丢缓存；知识列表读取存在串行放大【静态确认】

根视图用 switch + 页面 identity 替换工作区。知识的文档缓存、列表缓存和选中项是页面 `@State`，AI Day Store 是页面 `@StateObject`：离开工作区后不能视为常驻缓存，回来需重新建立。AI Day 的 420 秒新鲜度策略因此仅在该 Store 生存期间有效。

知识 `loadItems()` 不先用已有库列表返回；平铺加载串行遍历所有库，还可能与当前库的独立加载重叠。不能把知识说成完全没有缓存：同一次页面驻留内的文档读取确实命中缓存且有取消/过期结果检查。

建议数据与阅读位置上移到工作区级模型，返回先显示缓存；对跨库读取共享进行中的请求，按需展开或限制并发加载，保留缓存失效规则。

来源：[页面替换](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:781)、[知识页面状态](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopKnowledgeView.swift:23)、[知识列表读取](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopKnowledgeView.swift:1594)、[AI Day Store](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopAIDayView.swift:14)。

### F10 · P2 · 搜索清空旧结果后等待最慢的搜索域【静态确认】

搜索已有 200 ms debounce、取消检查和 query 一致性保护，但在 debounce 前就清空所有结果。任务、会话、知识和记忆请求并发启动，结果全部完成后才一起赋值。会话和知识最多读取 200 条，最终每类只显示 6 条；会话有去重需求，不能盲目把后端 limit 改成 6。

建议保留上一次结果并明确刷新态，各域独立显示；任务可优先基于内存检索，远程/磁盘结果随后补充；去重和排序尽量由支持分页的接口承担。实际最慢域需单独测量。

来源：[搜索流程](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopSearchPalette.swift:563)、[后端读取范围](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:2995)。

### F11 · P2 · IPC 大结果经过多轮 JSON 转换；短命令完成靠轮询发现【静态确认】

`validatedPayload()` 先 JSONDecoder 校验 envelope，再 JSONSerialization 建完整对象树、提取并重新序列化 data，最后再次 JSONDecoder 解业务对象。对本机约 2.58 MB 的全量快照，会产生额外遍历与临时对象。命令 runner 每 20 ms 检查一次进程结束，对短命令附加完成检测粒度和唤醒成本。

建议用一次 typed envelope 解码完成版本/verb/错误校验和 payload 解码；用进程终止回调及异步管道收集替代定时检查，保留 timeout、取消及 stderr 防阻塞。**runner 已在 detached task 中运行，这不是主线程 waitUntilExit 问题。** 本轮没有单独量化 JSON 的耗时比例。

来源：[IPC 转换](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMIPC.swift:333)、[进程 runner](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:395)。

### F12 · P2 · 收集行构建反复扫描整批记录【静态确认；记录较多时】

`itemRow` 每行通过 `selectedItem → displayedItems → primaryItems → groupedItems` 重算选中项，同时 supplements 再筛选整批记录并排序。来源分组循环内也反复读取 primaryItems。200 条快照上限限制了当前规模，但按行重复全量处理仍近似二次增长，并随全局 Store 发布重复执行。

建议一次生成 source 分桶、todo 补充记录索引、未读计数和有效 selectedID，行仅做 O(1) 查询；收集已读操作的刷新应限定到受影响记录。

来源：[列表派生属性](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopCollectionView.swift:78)、[行构建](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopCollectionView.swift:624)、[补充记录扫描](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/CollectionModels.swift:661)。

### F13 · P2 · 长输入框每次输入/改尺寸都强制全文布局【静态确认；长输入场景】

增长式编辑器在 didChangeText 和 setFrameSize 调用 `ensureLayout(for: container)`，先完成文本容器布局再限制显示高度。最大显示行数不会限制测量工作量。新建任务同时会依据整份任务集合计算项目建议，body 中 draft 和 suggestion 分别进入推断路径。

建议增长到高度上限后停止全文高度测量，固定高度滚动；避免无变化的字体、inset 写回触发额外布局。项目候选按任务版本缓存，当前文本推断只算一次。短标题输入不应优先于任务切换和全局刷新处理。

来源：[高度测量](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMComposerTextView.swift:271)、[重复建议计算](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/DesktopContentView.swift:2661)。

### F14 · P3 · 常驻 Store 的完整会话缓存没有容量限制【条件性风险】

`sessionTranscripts`、`sessionTimelines` 按 session/mode 常驻保存，未见按内存压力、总字符数或最近使用时间淘汰。多次查看很长的完整会话后，内存可以随访问集合增长。没有发现当前内存泄漏证据；这是缓存预算缺失，不等于对象泄漏。

建议按字节预算和最近使用淘汰，保留当前阅读项；离开读取页可取消无消费者任务。保留已有摘要/完整/时序分开读取及缓存命中机制。

来源：[缓存字段](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:930)、[缓存读取](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMDataStore.swift:3192)。

### F15 · P3 · 语音粘贴存在明确的主线程 50 ms 阻塞【静态确认；语音完成时】

`ATMTextInjector` 是 MainActor 类型，模拟粘贴的按下/松开之间执行 `usleep(50_000)`。同一流程还在主线程复制剪贴板全部类型，剪贴板含大图片时有额外风险。该路径仅在语音输入完成时触发，不能解释普通任务切换卡顿。

建议把键盘事件间隔改为可挂起的延迟，保留按下/松开的顺序及剪贴板恢复协议；剪贴板读取范围和大数据恢复策略另行评估。

来源：[主线程粘贴](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMTextInjector.swift:14)、[同步休眠](/Users/mj/mox/atm/app/macos/Sources/ATMMenuBarApp/ATMTextInjector.swift:109)。

## 已有优化与排除项

- CLI runner 使用后台 detached task，stdout/stderr 分开读，并有超时和取消机制；没有把所有 CLI 调用判定为主线程阻塞。
- `DesktopUsageContent.equatable()` 以统计相关 render key 隔离无关 Store 发布，统计页的做法可以推广。全量统计查询仍重，但不等于统计图表每次 Agent 更新都重绘。
- `primeWork()` 已有冷启动轻量首屏，问题在其覆盖范围和后续全量刷新，而不是完全没有首屏优化。
- 知识文档在当前页面驻留期间缓存，搜索有 debounce 和取消，Agent 索引按 200 条分页；这些路径无需从零重做。
- `ATMTranscriptTextView` 的纯文本查看器避免重复赋值，TextKit 1 开启非连续布局；F07 指向另一套结构化完整会话视图。
- SenseVoice 大文件校验和解压已放入 detached task，识别引擎有独立 actor；不能因为看到 `waitUntilExit()` 就判定 UI 被它阻塞。
- Agent 活跃呼吸动画限定在选中详情小组件，普通日志异步写出；本轮没有足够依据把阴影、配色或所有动画都列为主要瓶颈。
- 没有运行功能回归测试，因为本轮未修改实现。静态和采样检查不替代未来修复后的性能、状态保持和取消行为验证。

## 建议实施顺序和验收

1. **先消除任务切换的重复计算**：F02 + F03；给任务分组和页面状态增加明确版本边界，复测短文任务切换。保留编辑状态和选中语义。
2. **拆分重后台刷新**：F01；work 先独立到达，统计按需加载。只看任务时不反复聚合 7 个统计范围；对旧响应、乐观更新和 sync 后刷新做回归验证。
3. **处理切换附带工作**：F04 + F05 + F06 + F08；缓存正文和缩略图、按需读取、稳定详情壳。缩短动画应与实际 CPU 改善分开验收。
4. **推广到其他页面**：F07 + F09 + F10 + F12；完整会话按轮次虚拟化，跨页保留模型，搜索逐域出结果，收集预索引。
5. **处理管线及长时间使用风险**：F11 + F13 + F14 + F15。

建议建立同机 Release 基准：短文/长文/图片任务各 20 次切换，记录点击到选中反馈、正文首帧和稳定布局的 p50/p95，并对照 CPU 主线程样本；再验证数百任务、大分组、长会话、跨页面返回和后台刷新重叠。目标可先定选中反馈 p95 < 50 ms、缓存详情首帧 p95 < 100 ms，作为待验收预算，**不是本轮已达到或已测得的结果**。

首次只读排查没有匹配的既有 Todo，保持未绑定。后续修复阶段由 `t355`「修复 ATM macOS 客户端性能卡顿」记录；本文的问题描述和测量结果是修复前基线，最终实施与验收证据以修复记录为准。
