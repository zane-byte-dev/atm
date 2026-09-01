# ATM UI Language v1

状态：方案稿  
范围：macOS 桌面端  
目标：把任务、收集、Agent、知识、用量和 AI Day 收敛为同一种产品语言，并优先复用现有公共组件。

## 1. 一句话定义

ATM 是一张安静、可靠、可持续工作的 AI 工作台，不是由卡片拼成的数据后台。

界面始终回答三个问题：

1. 我在哪里？——左栏负责全局位置。
2. 我正在看什么？——中栏负责集合、分组和选择。
3. 我能理解或处理什么？——右栏负责内容、状态和动作。

## 2. 设计原则

### 2.1 内容优先

容器只在表达边界、归属或可操作对象时出现。页面分区不默认加卡片；阅读内容优先使用连续画布。

### 2.2 一件事只表达一次

选中态只用一种主要信号：选中填充。不要同时叠加蓝色竖条、描边、阴影和字重跳变。

### 2.3 骨架稳定，语义变化

不同业务页面共享相同的列宽、标题基线、列表节奏、分页控件和状态反馈。变化只来自图标、字段和内容类型，不来自重新发明布局。

### 2.4 关系强于装饰

间距、对齐和层级先建立关系；色块、边框和阴影只作为补充。默认无阴影，只有浮层和真正抬升的临时对象使用阴影。

### 2.5 渐进披露

首屏给结论、当前状态和下一步；原始日志、技术字段、次要操作收进展开区、菜单或次级分页。

### 2.6 原生而克制

遵循 macOS 的密度、键盘操作和可访问性，保留 ATM 的蓝灰左栏与冷中性色；避免网页式大圆角、重渐变和高饱和装饰。

## 3. 页面骨架

### 3.1 Workspace Shell

桌面工作区统一为三列：

| 区域 | 角色 | 默认宽度 | 行为 |
| --- | --- | ---: | --- |
| Rail | 全局导航 | 160 pt，可折叠到 58 pt | 跨页面稳定，不承载页面详情 |
| Navigator | 集合与选择 | 336 pt，可在 300–420 pt 调整 | 所有业务页共享同一组件语法 |
| Canvas | 阅读与操作 | 剩余空间，最小 520 pt | 内容按阅读型、对象型、数据型组合 |

顶栏、Rail 与 Navigator 是应用骨架；Canvas 才是业务内容。

### 3.2 Navigator 的统一契约

任务、收集、Agent、知识使用同一个 `ATMGroupedNavigator` 组合，而不是四套相似列表。

固定度量：

- Header：64 pt，高度不随页面变化。
- Group Header：32 pt，承载折叠、语义色、标题、数量和尾部动作。
- Content Row：最小 64 pt，外边距 8 / 2 pt，圆角 10 pt。
- Header 水平内边距 20 pt；滚动内容水平内边距 12 pt、垂直内边距 8 pt；组间距 4 pt。
- 主文本：13 pt Medium，最多两行。
- 辅助文本：12 pt Regular，最多一行。
- 选中：Accent 8% 填充；无蓝条、无描边、无阴影、不改变字重。
- Hover：4% 主文字色填充；Selected 优先于 Hover。
- 键盘：上下移动、左右折叠、Return 打开、Space 触发主动作。

允许变化的只有行内容插槽：

- `leading`：状态点、文档图标、Agent 标记或任务编号。
- `title`：主标题。
- `subtitle`：来源、摘要、负责人或时间。
- `trailing`：时间、数量、状态或菜单。
- `accessory`：仅在确有第二行结构时使用。

### 3.3 Canvas 的三种内容模式

| 模式 | 适用页面 | 规则 |
| --- | --- | --- |
| Reading | 知识文章、记录原文 | 最大阅读宽度 900 pt；标题和正文同流滚动；少用卡片 |
| Object | 任务、Agent、收集详情 | 固定详情头 + 分页 + section；卡片只包围独立对象 |
| Data | 用量、健康度 | 指标条 + 图表面板 + 明细；颜色服务于比较和状态 |

## 4. 视觉基础

### 4.1 颜色角色

继续以 `ATMTheme` 为唯一来源，不在页面中直接写 RGB 或系统颜色。

| Token | 用途 |
| --- | --- |
| `rail` / `railSelected` | 全局导航背景与选择 |
| `listPane` | 中栏连续底面 |
| `canvas` | 右栏主画布 |
| `elevated` | 阅读纸张、独立对象、浮起的选中分段 |
| Accent 8% | 中栏内容行选择 |
| `segmentTrack` | 分段控件底板 |
| `border` / `borderStrong` | 有真实边界的对象与强分隔 |
| `accent` | 当前选择、主动作、信息态 |
| `success` / `warning` / `danger` | 结果和风险，不用于分类装饰 |
| `palette` | 图表分类系列，不进入普通导航 |

语义色必须同时配合文字、图标或形状，不能只靠颜色传达状态。

### 4.2 字体阶梯

沿用 `ATMFont`，减少页面自行选择字号：

| Tier | 典型用途 |
| --- | --- |
| 10 micro | 极小角标 |
| 11 caption | 分组、表头、标签 |
| 12 footnote | 元数据、说明、次级按钮 |
| 13 body | 行标题、正文、控件 |
| 15 bodyLarge | 详情正文、section 标题 |
| 17 title3 | 小面板标题 |
| 20 title2 | 详情标题、中栏主标题 |
| 22 title1 | 页面级阅读标题 |
| 26 metric | 指标数值 |
| 32 display | 空状态图标、大数字 |

数字使用 `monospacedDigit()`；ID、路径、模型名按语义使用 `ATMFont.mono`，不要通过缩小字号制造“技术感”。

### 4.3 间距与圆角

基础间距：4 / 8 / 12 / 16 / 24 / 32 pt。

| Token | 值 | 用途 |
| --- | ---: | --- |
| `space-1` | 4 | 图标与微标签 |
| `space-2` | 8 | 行内元素、小组件 |
| `space-3` | 12 | 控件组、卡片内容 |
| `space-4` | 16 | 面板内边距、中栏 header |
| `space-5` | 24 | 详情横向基线、section 间距 |
| `space-6` | 32 | 大区块间距 |

圆角只有四档：6 pt 控件、10 pt 行、12 pt 面板、16 pt 仅用于强调型大容器。不要按页面新增 7/8/9/14 pt 的近似值。

### 4.4 边框与阴影

- 连续画布：无边框、无阴影。
- 行选择：无边框、无阴影。
- 有界对象：1 pt `border`。
- 浮层、popover、被选中的分段：允许轻阴影。
- 同一容器不能同时依赖高对比填充、强边框和阴影。

### 4.5 动效

沿用 `ATMMotion`：Hover 100 ms、Disclosure 160 ms、Selection 200 ms、Workspace Swap 220 ms。

动效只解释状态从哪里到哪里；不使用弹簧回弹，不让布局因字重、描边或尺寸变化发生抖动。开启 Reduce Motion 时退化为短淡入淡出。

## 5. 组件架构

### 5.1 Foundation

已有并保留：

- `ATMTheme`：颜色和表面角色。
- `ATMFont`：字体阶梯。
- `ATMMotion`：动效节奏。
- `ATMDesktopLayout` / `ATMSplitColumn`：窗口与列宽。

建议补充：

- `ATMSpacing`：4/8/12/16/24/32。
- `ATMRadius`：6/10/12/16。
- `ATMGroupedNavigatorMetrics`：Header 64、Group 32、Row 64、Radius 10、Selected Fill 8%。
- `ATMGroupedNavigatorScroll`：统一滚动、内容边距与组间距；中栏不使用系统 `List(.sidebar)`。
- `ATMStroke`：普通与强分隔。

### 5.2 Primitive

已有并统一使用：

- `ATMIconButton` / `ATMIconMenuLabel`
- `ATMHoverLabelButton`
- `ATMCompactSegmentedTabs`
- `ATMCapsuleTabs`
- `ATMEmptyState`
- `ATMInlineNotice`
- `ATMBrandMark` / `ATMAgentMark`

约束：按动作性质、位置和语义选组件，不按页面选样式。

### 5.3 Navigator

已有基础：`ATMDrawerHeader`、`ATMDrawerDisclosureLabel`、`ATMRowSurface`、`ATMContentRowLayout`。

应收敛成一个公开组合：

```swift
ATMGroupedNavigator(
    header: ATMNavigatorHeader(...),
    groups: [ATMNavigatorGroup(...)]
) { item in
    ATMNavigatorRow(
        leading: ...,
        title: ...,
        subtitle: ...,
        trailing: ...
    )
}
```

业务页面只提供数据、分组和插槽；滚动、间距、选中、hover、键盘与空状态由组件负责。

### 5.4 Detail

已有基础：`ATMDetailHeader`、`ATMDetailLayout`、`ATMCapsuleTabs`、`ATMMarkdownContentView`。

建议补齐：

- `ATMDetailScaffold`：固定 header、tabs 与可滚动 body。
- `ATMMetadataStrip`：状态、项目、优先级、创建时间等键值。
- `ATMDetailSection`：标题、尾部动作和正文基线。
- `ATMCallout`：非错误类的重点结论或下一步。
- `ATMActionBar`：主动作、次动作和溢出菜单的稳定顺序。

### 5.5 Data

用量页形成可复用的数据组件，而不是页面私有卡片：

- `ATMMetricTile`
- `ATMStatStrip`
- `ATMChartPanel`
- `ATMQuotaRing`
- `ATMBreakdownRow`
- `ATMChartLegend`

指标卡只放一个主要数字、一个单位、一个变化或解释；避免在一张卡里放多个竞争焦点。

### 5.6 Feedback

- 加载：优先保留骨架和位置，局部显示进度。
- 空：`ATMEmptyState`，区分 pane 与 inline。
- 提示/错误：`ATMInlineNotice`，首屏给结论和下一步，原始内容可展开。
- 完成：就地反馈，不额外弹窗。
- 危险操作：只在不可逆时确认，并明确对象名和后果。

## 6. 统一状态语法

| 状态 | 表面 | 文字/图标 | 其他 |
| --- | --- | --- | --- |
| Rest | 透明或所属底面 | Primary / Secondary | 无阴影 |
| Hover | `primary` 4–8% | 提升到 Primary | 100 ms |
| Selected | Accent 8% | 图标可用 Accent | 不加蓝条、描边、阴影 |
| Focused | 系统 focus ring | 不改变布局 | 键盘可见 |
| Disabled | 所属底面 | Secondary 45% | 不响应 hover |
| Loading | 保留容器 | Secondary + progress | 不让页面跳动 |
| Warning | `warningFill` | Warning + 文本/图标 | 不只用颜色 |
| Error | `dangerFill` | Danger + 下一步 | 详情可展开 |

## 7. 页面组合

### 7.1 任务

- Navigator：按状态分组的 `ATMGroupedNavigator`。
- Row：任务 ID + 标题 + 项目/负责人 + 状态时间。
- Canvas：Object 模式；`ATMDetailHeader` + `ATMMetadataStrip` + Tabs + 描述/动态/会话。

### 7.2 收集

- Navigator：按来源分组，记录使用同一 Row 度量。
- Row：来源标记 + 标题 + 分类/时间。
- Canvas：Object 模式；先给处理结论、来源、置信度和主动作，再给原文。

### 7.3 Agent

- Navigator：按活跃/全部分组，不另造“会话卡片”风格。
- Row：Agent Mark + 任务标题 + 最新消息 + 活跃时间。
- Canvas：Object 模式；状态、用户输入、最新动态、进展与技术信息按 section 排列。

### 7.4 知识

- Navigator：知识库作为 group，文章作为统一 Row。
- Canvas：Reading 模式；900 pt 阅读栏，文章标题、来源和正文同流滚动。
- 编辑、复制和更多操作收进标题区操作栏，不用悬浮装饰卡片。

### 7.5 用量

- 无业务 Navigator 时允许 Canvas 全宽，但仍沿用页面 header、filter bar 和 section 基线。
- 结构：配额概览 → 过滤条件 → 关键指标 → 趋势 → 构成 → 空/异常状态。
- 图表颜色只代表系列；绿/橙/红保留给健康、警告和危险。

### 7.6 AI Day

- 允许更强的品牌表达，但继续使用同一 tabs、section、按钮、空状态和详情 sheet。
- 特殊视觉仅存在于徽章与星图内容，不扩散到普通工作区 chrome。

## 8. 响应式与可访问性

- Navigator 可拖拽 300–420 pt；Canvas 优先保留最小 520 pt。
- 窄宽度下操作区换行，不压缩标题到不可读。
- 正文最大宽度 900 pt，大屏不无限拉长行长。
- 所有 icon-only 控件有 tooltip 和 accessibility label。
- 点击目标至少 28 × 28 pt；高频行目标至少 44 pt。
- 支持键盘导航、VoiceOver、Reduce Motion 和系统浅色/深色切换。
- 重要状态不能只靠颜色；图表提供 legend、数值和可读文本。

## 9. 落地顺序

### Phase 1：冻结语言

补齐 `ATMSpacing`、`ATMRadius`，建立组件预览页和 light/dark 快照；禁止页面新增裸 RGB、私有分段控件和私有选中表面。

### Phase 2：统一中栏

实现 `ATMGroupedNavigator` / `ATMNavigatorRow`，依次迁移任务、收集、Agent、知识。迁移期间不改业务数据和交互逻辑。

### Phase 3：统一详情

引入 `ATMDetailScaffold`、`ATMMetadataStrip`、`ATMDetailSection`，先迁移任务与 Agent，再迁移收集；知识保留 Reading 模式。

### Phase 4：提取数据组件

从用量页抽出 Metric、Stat、Chart、Quota 组件，并给 AI Day/设置页可复用的接口。

### Phase 5：治理

增加 SwiftLint/脚本检查：页面层禁止直接颜色、任意圆角和重复的 hover/selected modifier；PR 模板要求提供浅色、深色、窄宽度和空/错误态截图。

## 10. 验收标准

完成后应满足：

- 在任务、收集、Agent、知识之间切换，中栏首行、分组和列表不发生视觉跳变。
- 同一个操作在所有页面使用同一种控件和 hover 反馈。
- 选中态没有蓝条、描边、阴影或字重抖动。
- 页面层不新增裸 RGB、任意圆角、私有 tabs 或私有 empty state。
- 浅色、深色、窄宽度、键盘、VoiceOver、Reduce Motion 均通过快照或人工验收。
- 用户能在 3 秒内识别当前位置、当前选择和下一步主动作。

## 11. 关键决策

1. 左栏保留 ATM 的品牌色，但不把品牌色扩散到每张卡片。
2. 中栏是同一个组件；页面差异来自数据和插槽。
3. 选中态只保留填充，取消蓝色竖条。
4. 右栏按 Reading / Object / Data 三种内容模式组合。
5. 优先整合现有组件，不做一次性大重写。
