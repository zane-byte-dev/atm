# AI Day Badge System v1

AI Day 徽章是一组“计算物件”，不是奖牌、图标或彩色贴纸。每枚徽章通过独立轮廓表达行为语义，同时共享同一套相机、光线与材质世界。

## 视觉语言

- 主材质：烟黑透明玻璃、清透晶体层压、少量石墨色金属微框架。
- 计算光：冷白与冰蓝，用于结构、光路和已验证信息。
- 用户核心：每枚物件仅保留一个小面积暖琥珀核心。
- 镜头：统一三分之四正交视角，确保在 64 px 下仍可通过剪影辨识。
- 避免：奖章外壳、盾牌、印章、SF Symbol、彩色圆底、紫色渐变、游戏道具质感。

## 造型语义

| ID | 中文名 | 核心结构 |
| --- | --- | --- |
| `autopilot` | 自动驾驶 | 彗星种子与自主轨道 |
| `deep_collaboration` | 深度共创 | 连接双核心的莫比乌斯共振体 |
| `model_conductor` | 模型指挥家 | 单束输入、棱镜路由与多束受控输出 |
| `visual_director` | 视觉导演 | 光圈、成像面与受控星云核心 |
| `code_architect` | 代码架构师 | 等距结构网格与上升计算塔 |
| `quality_inspector` | AI 质检员 | 扫描平面、缺陷显影与校准晶体 |
| `follow_up` | 追问者 | 向核心收敛的递归问题轨道 |
| `detail_microscope` | 细节显微镜 | 精密光学镜头与放大微结构 |
| `generalist` | 全能协作者 | 平衡多种模态的多面体装配体 |
| `hard_to_fool` | 不易被糊弄 | 偏折伪信号、放行真实光束的折射场 |
| `first_draft_accepted` | 一稿即中 | 内部路径一次闭合的完整晶体 |
| `streak` | 持续同行 | 上升星晶链与螺旋星图 |

## 生成与交付

最终资产使用内置图像生成能力制作，母版为透明背景 4 × 3 图集；`model_conductor` 与 `hard_to_fool` 使用单独生成的高分辨率资产。应用侧保留 SwiftUI Canvas 作为资源缺失时的降级方案。

- 母版：[ai-day-badge-atlas-v1.png](./ai-day-badge-atlas-v1.png)
- 应用资源：`app/macos/Sources/ATMMenuBarApp/Resources/AIDayBadges/`
- 主提示词方向：museum-grade cinematic 3D computational artifacts；smoke-black glass；cold white / ice-blue compute light；single warm amber core；transparent alpha；distinct silhouettes；no medals, shields, labels, icons, or cartoon-game styling。
