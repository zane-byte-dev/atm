# AI Day

AI Day 是 ATM 的本地每日回顾：每天只选择一个概念和一枚徽章，并给出 2–3 个可核验的数值证据。没有
可用活动时返回 `empty`，不会补造数据。

## 数据与隐私

`atm day today` 会把 ATM 已有会话镜像归一化为 `ai_day_events`。事件 ID 幂等，包含时间、来源、哈希
会话 ID、事件类型、模态、执行模式、token、时长和语义标签。语义分类只在本机短暂读取已有消息，保存
后原文立即丢弃；表约束要求 `raw_content_retained = 0`。默认衍生事件保留 90 天，日结果和徽章历史可
继续保留。

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

## 语义与 Reward

本地规则引擎支持八类多标签意图：`correction`、`retry`、`refinement`、`question`、`directive`、
`acceptance`、`brainstorm`、`explanation`。关闭语义后，非语义徽章仍可工作。

Reward 从 3–5 个候选中选择当天唯一结果。评分由显著性 40%、稀有度 25%、置信度 20%、新鲜度
15% 组成；即时徽章有 14 天冷却。徽章等级按有效日计算：L1 为首次高置信选中或累计 3 个有效日，
L2 为最近 60 天内 7 个有效日，L3 为累计 30 个有效日。

Atlas 固定包含 12 枚徽章：自动驾驶、深度共创、模型指挥家、视觉导演、代码架构师、AI 质检员、
追问者、细节显微镜、全能协作者、不易被糊弄、一稿即中、持续同行。

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
```

macOS 主窗口的 AI Day 工作区包含今日、Atlas 星图/列表、历史、数据与隐私四个页面。分享面板可选择
日期、证据和统计字段，并在本机导出 1080×1350 PNG；卡片只使用真实日结果。
