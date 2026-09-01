# AI Day Badge System v2 — Q 版精简

v2 延续 v1 的“计算物件”世界观，但将每枚徽章收敛为更友好、更易读的 Q 版收藏物件。重点不是增加细节，而是用一个清楚的轮廓表达一种行为。

## 精简规则

- 每枚只保留“一个主体 + 一个动作结构 + 一个暖色核心”。
- 使用圆润、短比例和略微夸张的体块，像一枚小型桌面收藏玩具。
- 造型控制在 3–5 个大组件，不使用微型机械框架、密集节点或多层光路。
- 材质仍为烟黑玻璃、冰蓝透明体与暖琥珀核心，但减少复杂折射和装饰性高光。
- 优先保证 64–96 px 下的剪影识别；锁定状态只降低饱和度，不改变造型。

## 语义保留

| ID | 保留的核心内容 |
| --- | --- |
| `autopilot` | 自主核心与单一环绕轨道 |
| `deep_collaboration` | 两端连接的圆润共创环 |
| `model_conductor` | 一个棱镜与少量受控输出 |
| `visual_director` | 单一镜头 / 光圈与成像核心 |
| `code_architect` | 简化平台与上升结构 |
| `quality_inspector` | 被扫描线穿过的校准晶体 |
| `follow_up` | 两条向核心收敛的追问轨迹 |
| `detail_microscope` | 一枚放大镜与一个观察对象 |
| `generalist` | 四种能力汇聚的圆角多面体 |
| `hard_to_fool` | 过滤器与真假信号分流 |
| `first_draft_accepted` | 一次闭合的清晰路径 |
| `streak` | 连续上升的星星轨迹 |

## 生成与交付

最终资产使用内置图像生成能力制作，母版为透明背景 4 × 3 图集，按固定网格切分为 12 个 362 × 362 PNG；应用继续使用原有资源 ID，因此不需要改动业务逻辑。

- 母版：[ai-day-badge-atlas-v2.png](./ai-day-badge-atlas-v2.png)
- 应用资源：`app/macos/Sources/ATMMenuBarApp/Resources/AIDayBadges/`
- 主提示词方向：premium chibi 3D collectible；rounded compact proportions；one main body, one secondary gesture, one warm amber core；smoke-black and ice-blue glass；3–5 large components；transparent alpha；no dense mechanical detail, labels, medals, shields, or complex scene rendering。
