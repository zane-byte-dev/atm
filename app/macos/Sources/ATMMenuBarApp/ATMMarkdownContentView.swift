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
    var sizing: Sizing
    @StateObject private var model: ATMMarkdownDocumentModel
    @State private var tablePaging = ATMMarkdownTablePaging()

    init(source: String, sizing: Sizing = .content) {
        self.source = source
        self.sizing = sizing
        _model = StateObject(wrappedValue: ATMMarkdownDocumentModel(source: source))
    }

    @ObservedObject private var appearance = ATMAppearance.shared

    private var bodySize: CGFloat {
        switch sizing {
        case .content: return appearance.contentTextSize.pointSize
        case .fixed(let tier): return tier.size
        }
    }

    var body: some View {
        let document = model.document.flatMap { $0.source == source ? $0 : nil }
            ?? ATMMarkdown.cachedDocument(source)
        Group {
            if let document {
                content(document)
            } else {
                ProgressView()
                    .controlSize(.small)
                    .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
            }
        }
        .preference(key: ATMWorkspaceContentReadyPreferenceKey.self, value: document != nil)
        .task(id: source) { await model.load(source) }
    }

    private func content(_ document: ATMPreparedMarkdown) -> some View {
        LazyVStack(alignment: .leading, spacing: 10) {
            ForEach(document.blocks.indices, id: \.self) { index in
                let block = document.blocks[index]
                switch block {
                case .heading(let level, let text):
                    Text(document.inline(text))
                        .font(.system(size: headingSize(level), weight: level <= 2 ? .bold : .semibold))
                        .padding(.top, level <= 2 ? 7 : 3)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                case .paragraph(let text):
                    markdownText(text, in: document)
                case .list(let ordered, let items):
                    LazyVStack(alignment: .leading, spacing: 6) {
                        ForEach(items.indices, id: \.self) { index in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(ordered ? "\(index + 1)." : "•")
                                    .font(.system(size: bodySize, weight: .semibold))
                                    .foregroundStyle(ATMTheme.secondary)
                                    .frame(minWidth: ordered ? 20 : 10, alignment: .trailing)
                                markdownText(items[index], in: document)
                            }
                        }
                    }
                    .padding(.leading, 2)
                case .taskList(let items):
                    LazyVStack(alignment: .leading, spacing: 6) {
                        ForEach(items.indices, id: \.self) { index in
                            let item = items[index]
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Image(systemName: item.checked ? "checkmark.square.fill" : "square")
                                    .font(.system(size: bodySize, weight: .medium))
                                    .foregroundStyle(item.checked ? ATMTheme.accent : ATMTheme.secondary)
                                markdownText(item.text, in: document)
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
                        markdownText(text, in: document)
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
                        ScrollView(.horizontal) {
                            Text(content)
                                .font(.system(size: bodySize, design: .monospaced))
                                .fixedSize(horizontal: true, vertical: true)
                                .textSelection(.enabled)
                                .padding(10)
                        }
                        // Opts out of the app-wide hidden scroll bars: a code line
                        // clipped at the card edge looks like the whole line, so
                        // here the bar is the only sign there is more to the right.
                        .scrollIndicators(.visible, axes: .horizontal)
                        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
                    }
                case .table(let headers, let alignments, let rows):
                    let visibleCount = tablePaging.visibleCount(table: index, source: source, total: rows.count)
                    VStack(alignment: .leading, spacing: 8) {
                        ScrollView(.horizontal) {
                            Grid(horizontalSpacing: 0, verticalSpacing: 0) {
                                markdownTableRow(
                                    headers,
                                    alignments: alignments,
                                    isHeader: true,
                                    document: document
                                )
                                ForEach(0..<visibleCount, id: \.self) { row in
                                    markdownTableRow(
                                        rows[row],
                                        alignments: alignments,
                                        isHeader: false,
                                        document: document
                                    )
                                }
                            }
                        }
                        // As with code blocks: a table cut off mid-column needs to say so.
                        .scrollIndicators(.visible, axes: .horizontal)
                        .clipShape(RoundedRectangle(cornerRadius: 7))
                        .overlay(RoundedRectangle(cornerRadius: 7).stroke(ATMTheme.border))
                        if visibleCount < rows.count {
                            Button("显示更多（已显示 \(visibleCount) / \(rows.count) 行）") {
                                tablePaging.showMore(table: index, source: source, total: rows.count)
                            }
                            .buttonStyle(.plain)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.accent)
                        }
                    }
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

    private func markdownText(_ text: String, in document: ATMPreparedMarkdown) -> some View {
        Text(document.inline(text))
            .font(.system(size: bodySize))
            .lineSpacing(3)
            .fixedSize(horizontal: false, vertical: true)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func markdownTableRow(
        _ cells: [String],
        alignments: [ATMMarkdownTableAlignment],
        isHeader: Bool,
        document: ATMPreparedMarkdown
    ) -> some View {
        GridRow {
            ForEach(Array(cells.enumerated()), id: \.offset) { index, cell in
                Text(document.inline(cell))
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

/// A single rich-text paragraph, used by progress rows that already own their
/// layout. Parsing is shared with the document renderer but stays off the main
/// actor; modifiers on this view still control selection, font and color.
struct ATMMarkdownInlineText: View {
    let source: String
    @State private var prepared: Prepared?

    private struct Prepared {
        let source: String
        let text: AttributedString
    }

    private var currentText: AttributedString? {
        if let prepared, prepared.source == source { return prepared.text }
        return ATMMarkdown.cachedRender(source)
    }

    var body: some View {
        Group {
            if let text = currentText {
                Text(text)
            } else {
                ProgressView().controlSize(.mini)
            }
        }
        .task(id: source) {
            guard let text = await ATMMarkdown.prepareInline(source), !Task.isCancelled else { return }
            prepared = Prepared(source: source, text: text)
        }
    }
}
