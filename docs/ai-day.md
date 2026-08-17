# AI Day

AI Day 是 ATM 的本地每日回顾：每天只选择一个概念和一枚徽章，并给出 2–3 个可核验的数值证据。没有
可用活动时返回 `empty`，不会补造数据。

## 数据与隐私

`atm day today` 会把 ATM 已有会话镜像归一化为 `ai_day_events`。事件 ID 幂等，包含时间、来源、哈希
会话 ID、事件类型、模态、执行模式、token、时长和语义标签。语义分类只在本机短暂读取已有消息，保存
后原文立即丢弃；表约束要求 `raw_content_retained = 0`。默认衍生事件保留 90 天，日结果和徽章历史可
继续保留。

ATM 自身的模型调用（`agent='atm'`，例如内置分类器）不会被采集。它们是 ATM 代替你工作，而不是你
在与另一个 AI 协作；计入会凭空造出第二个「AI 来源」，让 `模型指挥家` 因为一次后台调用而达标。

```bash
atm day privacy show
atm day privacy set --semantic off
atm day privacy set --retention 30
atm day sources list
atm day sources pause codex
atm day sources resume codex
atm day sources delete codex --yes
atm day delete --from 2026-08-01 --to 2026-08-15 --yes
atm day delete --all --yes
```

暂停不会删除历史；来源删除会删除其 AI Day 衍生事件并同时暂停。范围删除只作用于 AI Day 的事件、结果
与反馈，不删除 ATM 原始会话索引。`atm day export` 始终输出 JSON，且不包含消息原文。

两处操作有超出字面含义的副作用，App 会先确认：

- `--semantic off` 不只是暂停开关。它会清空 `ai_day_events` 中已存的语义标签，并删除每日特征与
  会话特征投影。重新打开开关不会自动恢复，需要跑一次 `atm day rebuild` 重新分类。
- `--retention N` 会立即删除更早的衍生事件。日结果与徽章历史保留，但被删除的事件只有最近 31 天
  能通过重建从原始会话恢复，更早的无法恢复。

## 语义、模态与归因

本地规则引擎支持八类多标签意图：`correction`、`retry`、`refinement`、`question`、`directive`、
`acceptance`、`brainstorm`、`explanation`。关闭语义后，非语义徽章仍可工作。

模态只统计对话轮次和工具使用，每个事件计一次，不按数量加权。`usage` 事件不参与：把每次 API 调用
都算作一个 `general` 会让真实信号被淹没（整天写代码的一天曾经是 137 general 对 1 code，`代码架构师`
根本无法达标）。工具名比用户措辞可靠得多，是模态的主要来源。

`tools` 表是会话级累计值、没有事件时间戳（`PRIMARY KEY (session_id, name)`），所以日期只能推断。
每一行按会话在各天的 usage 事件占比拆分到这些天；没有 usage 可依据的会话回落到创建日。此前的规则
把整个会话的工具历史都记在创建那天，而同一会话的轮次和 token 按事件时间归日，同一天内存在两套口径。

Token 有两个口径：`work_tokens`（input + output + cache_create）和 `total_tokens`（含
`cache_read`）。缓存读取随上下文大小而非工作量增长，通常比其余部分大一到两个数量级，所以阈值、
百分位和「今天发生了多少」一律使用 `work_tokens`，缓存只作为附注展示。

## Reward

Reward 从候选中选择当天唯一结果。评分为显著性 80%、稀有度 12%、新鲜度 8%。此前的权重是显著性
40%、稀有度 25%、置信度 20%、新鲜度 15%，而置信度对任何有一个月历史的用户都是常数，所以实际是
一半看「你做了什么」、一半看「最近没发过什么」，徽章分布均匀是轮流发牌的结果。

即时徽章有 14 天冷却。`持续同行` 只在连续使用达到 7/14/30/60/100/200/365 天时播报，而不是任何
连续两天都参与竞选。

徽章等级按有效日计算：L1 为首次高置信选中或累计 3 个有效日，L2 为最近 60 天内 7 个有效日，L3 为
累计 30 个有效日。等级只统计 `collection_start_day`（首次运行 AI Day 的那天）之后的日子：首次运行
会回溯计算一个月基线，若一并计入进度，用户在看到第一张日卡之前 12 枚徽章就全部解锁了。基线本身
仍然使用完整窗口。

Atlas 固定包含 12 枚徽章：自动驾驶、深度共创、模型指挥家、视觉导演、代码架构师、AI 质检员、
追问者、细节显微镜、全能协作者、不易被糊弄、一稿即中、持续同行。

### 幂等性

同一份数据重复重建必须产生完全相同的历史。稀有度只统计**早于**当天的徽章记录；缺少这个过滤时，
它会把上一次运行为「之后的日子」写下的行也算进来，于是每次运行的输出成为下一次的输入——同一区间
重建两次会得到不同的过去（某天从「深度共创」变成「代码架构师」），且永不收敛。
`TestRepeatedRebuildIsByteIdenticalAcrossRuns` 对完整投影做三次快照比对。

### 可信度与用户纠正

- `evidence_strength`：当天这枚徽章自身的归一化信号强弱。
- `confidence`：这条结论有多可信 = 基线长度 30% + 证据强度 50% + 来源覆盖度 20%，未结束的当天
  再乘 0.8，上限 0.95。旧公式只与历史天数有关，因此任何满一个月的用户都固定显示 93%。
- `origin`：`computed` 或 `user_corrected`；`computed_id` / `computed_title` 保留纠正前引擎自己的
  判断。纠正表示这一天应该属于哪枚徽章，不表示引擎更有把握，所以不会把可信度抬到 100%，也不会
  用一条 `user_correction` 替换掉实测证据。
- 纠正可撤销：`atm day feedback <day> --clear` 删除该天反馈并恢复引擎结论。

## 当天与数据覆盖

未结束的当天标记 `provisional`，且不输出 `percentiles`：拿半天与 30 个完整天比较，会让任意一个
上午都排在自己基线的最低位（10:30 时 `total_tokens` 为 p0、`tool_calls` 为 p6）。依赖百分位的
判定分支在当天一律不参与。

`coverage` 把当天出现的来源与前 7 天活跃的来源比较。会话镜像由其他进程在会话落盘时填充，所以
「今天 0 次工具调用」经常意味着「还没同步」而不是「你没用工具」。`data_through` 是喂给这条结果的
最新事件时间。

## 重建范围

`atm day today` 和 `atm day dashboard` 走同一条 `Refresh`：补齐基线窗口内**从未构建过**的日子，
并且总是重建当天，已经构建过的过去保持不变。此前两者都会在每次读取时重建整个 31 天窗口，意味着
打开一次 App 就可能静默改写上个月的徽章。改写历史仍然可以做，但需要显式的 `atm day rebuild`。

## CLI 与 App

```bash
atm day today --json
atm day show 2026-08-15 --json
atm day rebuild --from 2026-08-01 --to 2026-08-15
atm day history --days 180 --json
atm day atlas --json
atm day badge code_architect --json
atm day feedback 2026-08-15 --verdict accurate
atm day feedback 2026-08-15 --verdict corrected --badge code_architect
atm day feedback 2026-08-15 --clear
```

macOS 主窗口的 AI Day 工作区包含今日、Atlas 星图/列表、历史、数据与隐私四个页面。今日卡显示
provisional 状态、数据截止与更新时间、覆盖度告警、证据强度与可信度，以及纠正来源。反馈按钮显示
当前状态并可撤销；纠正走「选择 → 预览 → 确认」，并标出当前徽章和当天本就达标的徽章。历史卡可
点击展开当天完整结论、证据、统计、其他达标徽章与分享入口，并支持按徽章筛选和月度趋势。窗口激活、
切回今日页或每 7 分钟会做一次轻量刷新。分享面板可选择日期、证据和统计字段，并在本机导出
1080×1350 PNG；卡片只使用真实日结果，并标注数据截止时间与 provisional 状态。
