import SwiftUI

/// 右栏详情顶部那条固定不滚动的头。
///
/// 任务、Agent、收集三页的详情头结构本来就同构——「眉标 + 操作」一行、标题一行、状态与
/// 属性在下面——但此前各写一遍：标题字号在 title1 / title2 / title3 之间各挑一个，内边距
/// 24/17/18 与 20/16 两套，底色 `elevated` 与 `surface` 两种。切页时标题基线和头部底色
/// 会一起变。现在度量只有这一份，内容仍归各页自己。
///
/// 知识页不用这个壳：它的标题不是固定头，而是文档流里的第一块，跟正文一起在 900pt 阅读栏
/// 内滚动；套上带底色的 band 会改掉那一页的阅读布局。那边只对齐标题字号。
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
    static let tabsHorizontalPadding: CGFloat = 16
    static let tabsVerticalPadding: CGFloat = 10
}
