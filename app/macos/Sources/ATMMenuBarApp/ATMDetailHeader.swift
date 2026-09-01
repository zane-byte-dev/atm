import SwiftUI

/// 右栏详情顶部那条固定不滚动的头。
///
/// 任务、Agent、收集三页的详情头结构本来就同构——「眉标 + 操作」一行、标题一行、状态与
/// 属性在下面——但此前各写一遍：标题字号在 title1 / title2 / title3 之间各挑一个，内边距
/// 24/17/18 与 20/16 两套，底色 `elevated` 与 `surface` 两种。切页时标题基线和头部底色
/// 会一起变。现在度量只有这一份，内容仍归各页自己。
///
/// 知识文档也使用这个壳：对象身份留在固定头，长文正文单独滚动。这样在任务、
/// Agent、收集和知识之间切换时，标题和全局操作不会随内容滚走。
struct ATMDetailHeader<Eyebrow: View, Actions: View, Meta: View>: View {
    let title: String
    var titleLineLimit: Int?
    let eyebrow: Eyebrow
    let actions: Actions
    let meta: Meta

    init(
        title: String,
        titleLineLimit: Int? = nil,
        @ViewBuilder eyebrow: () -> Eyebrow,
        @ViewBuilder actions: () -> Actions = { EmptyView() },
        @ViewBuilder meta: () -> Meta = { EmptyView() }
    ) {
        self.title = title
        self.titleLineLimit = titleLineLimit
        self.eyebrow = eyebrow()
        self.actions = actions()
        self.meta = meta()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            // 眉标（面包屑 / 会话身份 / 动作 chip）与操作同一行；窄栏时操作换行到下面，
            // 免得长项目名把按钮挤出可视区。
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .top, spacing: 12) {
                    eyebrow
                    Spacer(minLength: 10)
                    actions
                }
                VStack(alignment: .leading, spacing: 9) {
                    eyebrow
                    actions
                }
            }

            // 标题独占整行：状态一律排在标题下面而不是旁边——并排时换行的标题会把徽标顶离
            // 首行基线，在文字块里留一个豁口。
            Text(title)
                .font(ATMFont.font(.title2, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
                .lineLimit(titleLineLimit)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)

            meta
        }
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.top, 17)
        .padding(.bottom, 18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated)
    }
}

/// 右栏的横向节奏。头部、分页条和正文列必须共用同一条左边界，标题才不会比它解释的正文
/// 缩进浅一档。
enum ATMDetailLayout {
    static let horizontalPadding: CGFloat = 24
    /// The body card follows the same leading and trailing guides as the fixed
    /// header and tabs. A smaller, independent vertical gutter keeps it visually
    /// attached to the tab row without making the card touch it.
    static let surfaceHorizontalInset: CGFloat = horizontalPadding
    static let surfaceVerticalInset: CGFloat = ATMSpacing.medium
    /// Object details share one readable measure; only the information structure
    /// inside the column changes from tab to tab.
    static let contentMaxWidth: CGFloat = 880
    /// Tabs and every root content column share the header's leading edge.
    /// Keeping a second, smaller inset here made the capsule and body protrude
    /// 8pt to the left of the title (16px in Retina screenshots).
    static let tabsHorizontalPadding: CGFloat = horizontalPadding
    static let tabsVerticalPadding: CGFloat = 10
}
