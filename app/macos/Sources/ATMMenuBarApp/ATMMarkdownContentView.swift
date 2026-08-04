import SwiftUI

struct ATMMarkdownContentView: View {
    enum Sizing {
        /// Follows the reader's 正文字号 setting — task descriptions, knowledge
        /// documents, shared memories, progress entries, agent replies.
        case content
        /// Pinned to a chrome tier, for inline snippets that are read at a glance
        /// rather than sat down with.
        case fixed(ATMFont.Tier)
    }

    let source: String
    var sizing: Sizing = .content

    @ObservedObject private var appearance = ATMAppearance.shared

    private var bodySize: CGFloat {
        switch sizing {
        case .content: return appearance.contentTextSize.pointSize
        case .fixed(let tier): return tier.size
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(ATMMarkdown.blocks(source).enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    Text(ATMMarkdown.render(text))
                        .font(.system(size: headingSize(level), weight: level <= 2 ? .bold : .semibold))
                        .padding(.top, level <= 2 ? 7 : 3)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                case .paragraph(let text):
                    markdownText(text)
                case .list(let ordered, let items):
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(ordered ? "\(index + 1)." : "•")
                                    .font(.system(size: bodySize, weight: .semibold))
                                    .foregroundStyle(ATMTheme.secondary)
                                    .frame(minWidth: ordered ? 20 : 10, alignment: .trailing)
                                markdownText(item)
                            }
                        }
                    }
                    .padding(.leading, 2)
                case .taskList(let items):
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Image(systemName: item.checked ? "checkmark.square.fill" : "square")
                                    .font(.system(size: bodySize, weight: .medium))
                                    .foregroundStyle(item.checked ? ATMTheme.accent : ATMTheme.secondary)
                                markdownText(item.text)
                                    .foregroundStyle(item.checked ? ATMTheme.secondary : ATMTheme.primary)
                            }
                        }
                    }
                    .padding(.leading, 2)
                case .quote(let text):
                    HStack(alignment: .top, spacing: 10) {
                        RoundedRectangle(cornerRadius: 1)
                            .fill(ATMTheme.accent.opacity(0.55))
                            .frame(width: 3)
                        markdownText(text)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .padding(.vertical, 3)
                case .divider:
                    Divider()
                        .padding(.vertical, 5)
                case .code(let language, let content):
                    VStack(alignment: .leading, spacing: 6) {
                        if let language, !language.isEmpty {
                            Text(language.uppercased())
                                .font(ATMFont.mono(.caption, .semibold))
                                .foregroundStyle(ATMTheme.secondary)
                        }
                        ScrollView(.horizontal, showsIndicators: true) {
                            Text(content)
                                .font(.system(size: bodySize, design: .monospaced))
                                .fixedSize(horizontal: true, vertical: true)
                                .textSelection(.enabled)
                                .padding(10)
                        }
                        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
                    }
                case .table(let headers, let alignments, let rows):
                    ScrollView(.horizontal, showsIndicators: true) {
                        Grid(horizontalSpacing: 0, verticalSpacing: 0) {
                            markdownTableRow(
                                headers,
                                alignments: alignments,
                                isHeader: true
                            )
                            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                                markdownTableRow(
                                    row,
                                    alignments: alignments,
                                    isHeader: false
                                )
                            }
                        }
                    }
                    .clipShape(RoundedRectangle(cornerRadius: 7))
                    .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Fixed deltas, so the heading spread stays constant as body size grows
    /// rather than compressing toward it.
    private func headingSize(_ level: Int) -> CGFloat {
        switch level {
        case 1: return bodySize + 11
        case 2: return bodySize + 6
        case 3: return bodySize + 3
        default: return bodySize + 1
        }
    }

    private func markdownText(_ text: String) -> some View {
        Text(ATMMarkdown.render(text))
            .font(.system(size: bodySize))
            .lineSpacing(3)
            .fixedSize(horizontal: false, vertical: true)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func markdownTableRow(
        _ cells: [String],
        alignments: [ATMMarkdownTableAlignment],
        isHeader: Bool
    ) -> some View {
        GridRow {
            ForEach(Array(cells.enumerated()), id: \.offset) { index, cell in
                Text(ATMMarkdown.render(cell))
                    .font(.system(size: bodySize, weight: isHeader ? .semibold : .regular))
                    .lineSpacing(2)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .frame(
                        minWidth: 100,
                        maxWidth: 320,
                        minHeight: 32,
                        alignment: tableAlignment(alignments[index])
                    )
                    .textSelection(.enabled)
                    .background(isHeader ? ATMTheme.controlFill : Color.clear)
                    .overlay(Rectangle().stroke(ATMTheme.border, lineWidth: 0.5))
            }
        }
    }

    private func tableAlignment(_ alignment: ATMMarkdownTableAlignment) -> Alignment {
        switch alignment {
        case .leading: return .leading
        case .center: return .center
        case .trailing: return .trailing
        }
    }
}
