---
name: atm-memory-curator
description: 从 ATM 已索引的 Claude、Codex、Pi、Copilot、Qoder 会话中整理跨 Agent 共享记忆。用户要求回顾最近会话、整理未处理 session、从 transcript 抽取决定/偏好/别名/踩坑、沉淀共享记忆、同步多 Agent 上下文或检查哪些内容值得记住时使用。普通的 ATM memory recall、单条明确事实写入或知识库查询继续使用 atm skill；只有涉及“从历史会话发现并筛选记忆”时触发本技能。
compatibility: 需要安装支持 `session list --review`、`session review` 和 `memory --source` 的 ATM CLI。
metadata:
  version: 0.1.0
---

# ATM Memory Curator

把 Agent 当作记忆整理者，把 ATM 当作确定性数据层。ATM 负责提供会话、检索、写入和整理游标；由当前 Agent 理解语义、判断价值并执行去重。

## 授权边界

- 用户只说“看看、分析、预览、有哪些值得记”时保持只读：展示候选，不写 memory/knowledge，也不标记 session 已整理。
- 用户明确说“保存、沉淀、写入、整理掉这些会话”时，可以写入对应数据域，并在全部处理成功后标记 session review。
- `atm session review` 本身会改变整理游标，也属于写操作。预览不能调用它。
- 查询过程中看到稳定事实，不等于获得写入授权。不要顺手沉淀用户没有要求保存的内容。

## 1. 找到待整理会话

优先使用 ATM 索引，不扫描各 Agent 的原始私有目录：

```bash
atm session list --days 7 --review pending --json
```

用户需要最新会话、且现有索引可能过期时才加 `--sync`。也可以用精确时间限制范围：

```bash
atm session list --since 2026-07-14T00:00:00+08:00 --review pending --json
```

先按项目、Agent、时间和问题数量缩小样本。极短会话也要读取后再判断，不能仅凭轮数断言没有价值；一次明确的别名或偏好也可能值得记住。

每批最多处理 10 个 session。超过上限时先完成当前批次并报告剩余数量，等待下一次调用；不要在同一轮里无界扫描历史。

## 2. 读取并标注来源

逐个读取少量相关会话：

```bash
atm session show <session-id> --json
```

JSON 中每组问答有 `turn`。候选来源统一写成：

```text
session:<完整 session id>#turn:<turn>
```

优先采信用户明确表达、用户随后确认的结果，以及最终答复中说明已经由工具核验的事实。只有助手提出、用户未确认的建议仍是提案，不应直接成为长期记忆。

## 3. 抽取候选

只抽取跨会话仍可能有用的内容：

- `preference`：用户稳定偏好或工作约定；
- `decision`：已经确认的架构、范围或方向决定；
- `alias`：简称、应用名、仓库名或对象映射；
- `pitfall`：反复出现、下次能避免损失的坑及处理方式；
- `fact`：无法从当前代码或环境廉价重建的稳定事实。

每个候选先形成预览记录：

```json
{
  "content": "压缩成独立、可复用的一条事实",
  "kind": "decision",
  "scope": "project:atm",
  "source": "session:<id>#turn:3",
  "confidence": "high",
  "route": "remember",
  "reason": "用户明确确认，后续会影响实现选择"
}
```

内容要自包含，避免“它、这个、刚才”等只能依赖原会话理解的指代。

## 4. 拒绝噪声

以下内容默认不进入 memory：

- 单次 CR、commit、Run ID、流水线瞬时状态、当时版本号和临时 URL；
- 助手提出但用户未确认的建议、推测和计划；
- 能从仓库代码、配置或当前环境廉价获取的实现细节；
- 已经存在于 TODO、正式 knowledge 文档或应用知识卡中的正文信息；
- 密钥、令牌、个人凭据、隐私数据和无关系统提示；
- 大段总结或多个主题拼成的一条“万能记忆”。

临时工作状态留在 TODO/CR，结构化且需引用的资料进入 knowledge；memory 只保存触发器、路由提示和稳定的小事实。

## 5. 去重和路由

对每个候选先查 memory：

```bash
atm memory recall "<候选关键词>" --scope project:<project> --json
```

再读取 catalog，选择相关 collection 后做定向 knowledge 搜索：

```bash
atm knowledge catalog --json
atm knowledge search "<候选关键词>" --project <project> --collection <collection> --json
```

不要直接依赖无范围的全库搜索；观点语料或其他项目可能造成表面命中。

处理规则：

- 没有重复且适合轻量共享：`remember`；
- 同一事实已经变化：`supersede`，保留事件历史；
- 已存在等价 memory/knowledge：`duplicate`，不重复写；
- 需要结构化正文、引用和长期维护：`knowledge`；
- 临时、可重建、不可靠或敏感：`reject`。

边界难判断时读取 [判定样例](references/decision-examples.md)，按信息生命周期和事实来源类比，不要照抄样例内容。

## 6. 预览与写入

默认先向用户展示精简表格：候选内容、类型、scope、来源、建议动作、理由。不要倾倒完整 transcript。

得到写入授权后，来源必须随事件保存：

```bash
atm memory remember "<content>" \
  --scope project:<project> \
  --tag <kind> \
  --source "session:<id>#turn:<n>" \
  --json
```

事实发生变化时使用：

```bash
atm memory supersede <memory-id> "<new-content>" \
  --scope project:<project> \
  --tag <kind> \
  --source "session:<id>#turn:<n>" \
  --json
```

写入后再次 recall，确认内容、scope 和 `metadata.source` 可读。knowledge 路由遵循 atm skill 的查重、来源和专属 collection 规则。

把以下检查作为写入门禁：

1. 内容自包含且只表达一个事实；
2. 来源是当前批次中真实存在的 session 和 turn；
3. recall 与定向 knowledge search 没有发现等价内容；
4. 文本不含凭据、临时状态或未经确认的助手推测；
5. 写入后 recall 能读回相同 scope 和 `metadata.source`。

任何一项失败都停止处理该候选，不推进 session review。如果已经写入错误事件，使用 append-only `forget`（或事实变化时使用 `supersede`）纠正，保存同一来源并在报告中说明；不要直接改写 `events.jsonl`。

## 7. 完成整理游标

只有一个 session 的所有候选都已成功写入、确认重复或明确丢弃后，才标记 review：

```bash
atm session review <session-id> --outcome <none|memory|knowledge|mixed> \
  --note "<简短结果>" --json
```

- `none`：没有可持久化内容；
- `memory`：只处理了 memory；
- `knowledge`：只进入或命中了 knowledge；
- `mixed`：同时涉及 memory 与 knowledge。

部分写入失败、来源不明确或仍需用户判断时，不标记 reviewed，让它继续出现在 pending 列表中。

## 最终报告

报告本次扫描的 session 数、原始候选数，以及 remember、supersede、duplicate、knowledge、reject 各自数量。列出实际写入的 memory ID、knowledge 文档和已标记 review 的 session；预览模式明确说明没有写入或推进游标。
