# AI Day Badge System v3 — Frosted Jelly Glyphs

v3 将徽章从“微型 3D 物件”进一步收敛为“半透明果冻符号”。它参考了柔和磨砂玻璃图标的视觉语言，但没有复制参考图中的具体图形、卡片或文字。

## 视觉原则

- 统一正视图，不再使用复杂的三分之四透视。
- 每枚只保留一个主轮廓，最多一个内部符号或动作结构。
- 使用低饱和雾蓝磨砂玻璃、细深蓝轮廓和柔和奶油色内发光。
- 以圆角、短比例和大留白形成 Q 感，不使用表情或卡通角色化处理。
- 避免机械框架、多节点、切面晶体、黑色高光和电影化渲染。
- 在 64–96 px 下先读轮廓，再读暖色核心。

## 十二枚核心轮廓

| ID | v3 造型 |
| --- | --- |
| `autopilot` | 圆形种子与单一轨道 |
| `deep_collaboration` | 两端汇合的柔软无限环 |
| `model_conductor` | 圆角三角与三条短输出 |
| `visual_director` | 单一眼形光圈 |
| `code_architect` | 三块圆角积木组成的小塔 |
| `quality_inspector` | 圆形晶体与一条扫描线 |
| `follow_up` | 两条追问弧线汇入中心点 |
| `detail_microscope` | 圆润放大镜与一个观察点 |
| `generalist` | 四瓣能力汇入一个中心 |
| `hard_to_fool` | 过滤环与单一可信输出 |
| `first_draft_accepted` | 圆角方晶体内的一次闭合路径 |
| `streak` | 向上的柔软 S 轨迹与三颗星 |

## 生成与交付

资产使用内置图像生成能力、用户提供的风格参考图和透明背景后处理制作。透明边缘采用逐枚 Vision 前景分割与高阈值颜色遮罩组合：前者负责平滑外轮廓，后者只补回分离的小组件与内部暖光，避免棋盘残留和白色毛边。母版为 4 × 3 图集，按固定网格切分为 12 个 362 × 362 PNG。应用资源 ID 保持不变。

- 母版：[ai-day-badge-atlas-v3.png](./ai-day-badge-atlas-v3.png)
- 应用资源：`app/macos/Sources/ATMMenuBarApp/Resources/AIDayBadges/`
- 主提示词方向：soft frosted blue jelly-glass glyphs；frontal orthographic view；single silhouette；thin navy rim；gentle warm ivory glow；1–3 large components；no cards, text, mechanics, faceted crystal, or complex rendering。
