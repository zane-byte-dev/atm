---
description: ATM CLI 助手 — 查询和管理 AI 工作状态与待办任务
---
ATM 是跨所有 AI 编码工具（Claude Code / Codex / Copilot / pi / Qoder）的统一视图：
一个命令看到全部 agent 在干什么、历史会话、token 花费，以及共享的待办清单。

> 安装：把本文件复制到 `~/.pi/agent/prompts/atm.md`（pi 用户）；其他 agent 作 prompt/skill 参考。
> 前提：`atm` 已在 PATH 中（见项目 README）。详细参数一律 `atm <cmd> --help`。

## 触发词

"atm"、"看任务"、"查状态"、"工作进展"、"待办"

## 何时用什么

| 用户意图 | 命令 |
|---|---|
| AI 现在在干嘛 | `atm session status` |
| 最近做了什么 | `atm session list --days N` 或 `atm report [date]` |
| 找某次对话 | `atm session search <keyword>` |
| 用量 / 花了多少钱 | `atm stats [--days N]` |
| 我现在该关注什么 | `atm todo match --prompt`（当前仓库）/ `atm now`（全局面板） |
| 有哪些待办 | `atm todo list`（默认 open）|
| 某项目待办 | `atm todo list --project X` |

## 关键链路：先拿 id 再看详情

`session show` / `todo show` 都需要 id。**不要凭空编 id**，先查再看：

```bash
atm session list --days 3        # 或 search，拿到 short_id
atm session show <short_id>      # 再看完整 Q/A（加 --thinking 看思考过程）
```

## 只读 vs 写操作

- **只读**（status/list/search/show/stats/report/now/todo list/show）：默认只读 SQLite，无副作用；用户明确要刷新时加 `--sync`。
- 当前正在推进任务的 `start/log/edit/wait/submit` 属于正常生命周期同步，不需要用户重复提醒；
  `done` 是人的验收动作，Agent 不代替用户执行。
- `add` 前先查重并遵守任务数量控制；`drop/delete` 或修改无关任务必须由用户明确要求。

## 任务生命周期

```bash
atm todo add "<title>" --project X --priority P1   # 新建
atm session bind <id>                              # 当前会话接手并开始
atm session current                                # 查看当前绑定
atm todo log "<关键里程碑>"                         # 已绑定时省略 ID
atm todo submit --reason "<结果与证据>"             # Agent 完成实现，进入 review 并自动解绑
```

`review` 表示等待人验收；验收通过后由人执行 `atm todo done --reason "<结论>"`。

TODO 只保留一套生命周期状态；当前会话焦点由 session 绑定表达：

```bash
atm todo start <id>
atm todo wait <id> --wake "外部状态变化"
atm todo maintain <id> --limit 3
atm now
```

关联外部资源时保存完整 URL；ATM 只维护关联，CR/MR/流水线的查询和处理仍由对应 Agent/Skill 完成：

```bash
atm todo link add <id> "https://..." --kind cr --title "发布 CR"
atm todo link list <id>
```

不要保存带 token、签名或临时凭证的 URL。

## 输出约定

- 默认输出给人看；要程序化处理（pipe 到 jq、喂给后续步骤）就加 `--json`。
- `--agent claude|codex|copilot|pi|qoder|qodercli|qoderwork` 可按工具过滤（大多数查询命令通用）。
