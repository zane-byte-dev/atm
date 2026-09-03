import SwiftUI

struct TodoProgressView: View {
    let todo: ATMTodo
    let store: ATMDataStore
    @ObservedObject private var taskState: ATMTaskState
    @ObservedObject private var appearance = ATMAppearance.shared
    @State private var showingAllEntries = false
    @State private var progressLinkHovered = false

    private var entries: [ATMTodoProgressEntry] {
        store.progress(for: todo.id)
    }

    init(todo: ATMTodo, store: ATMDataStore) {
        self.todo = todo
        self.store = store
        _taskState = ObservedObject(wrappedValue: store.taskState)
    }

    private var visibleEntries: [ATMTodoProgressEntry] {
        // Avoid allocating a reversed copy of the whole history for a preview
        // that only displays its last three entries.
        showingAllEntries ? Array(entries.reversed()) : Array(entries.suffix(3).reversed())
    }

    var body: some View {
        let visibleEntries = self.visibleEntries
        VStack(alignment: .leading, spacing: 12) {
            if store.isLoadingProgress(for: todo.id), entries.isEmpty {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("正在加载动态…")
                }
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
            } else if entries.isEmpty {
                Text("暂无动态。AI 或你通过 atm todo log 记录进展后会显示在这里。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            } else {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(visibleEntries.enumerated()), id: \.element.id) { index, entry in
                        entryRow(entry, isLast: index == visibleEntries.count - 1)
                    }
                }
                if entries.count > 3 {
                    Button {
                        withAnimation(.easeInOut(duration: 0.16)) {
                            showingAllEntries.toggle()
                        }
                    } label: {
                        Text(showingAllEntries ? "收起历史" : "查看全部 \(entries.count) 条动态")
                            .font(ATMFont.font(.footnote, weight: .semibold))
                            .foregroundStyle(ATMTheme.accent)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(
                                ATMTheme.accent.opacity(progressLinkHovered ? 0.10 : 0.0),
                                in: RoundedRectangle(cornerRadius: 5, style: .continuous)
                            )
                    }
                    .buttonStyle(.plain)
                    .help(showingAllEntries ? "收起历史动态" : "展开全部历史动态")
                    .onHover { progressLinkHovered = $0 }
                }
            }
        }
        .task(id: todo.id) {
            store.loadProgress(for: todo.id)
        }
        .onChange(of: todo.id) { _ in showingAllEntries = false }
        .onChange(of: taskState.dataVersion) { _ in
            store.loadProgress(for: todo.id)
        }
    }

    private func entryRow(_ entry: ATMTodoProgressEntry, isLast: Bool) -> some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(spacing: 0) {
                Circle()
                    .fill(entryMarkerColor(entry))
                    .frame(width: 8, height: 8)
                    .padding(.top, 4)
                if !isLast {
                    Rectangle()
                        .fill(ATMTheme.border)
                        .frame(width: 1.5)
                        .frame(maxHeight: .infinity)
                }
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    if !entry.timestamp.isEmpty {
                        Text(entry.timestamp)
                            .font(ATMFont.mono(.caption, .semibold))
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    if entry.isDoneMarker {
                        Label("完成", systemImage: "checkmark.circle.fill")
                            .font(ATMFont.font(.caption, weight: .bold))
                            .foregroundStyle(ATMTheme.success)
                    }
                    if entry.kind == .supplement {
                        Text("补充")
                            .font(ATMFont.font(.caption, weight: .bold))
                            .foregroundStyle(ATMTheme.accent)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(ATMTheme.accentFill, in: Capsule())
                    }
                }
                ATMMarkdownInlineText(source: entry.text)
                    // Progress entries are read, not glanced at — they follow the
                    // 正文字号 setting like the other long-form areas.
                    .font(.system(size: appearance.contentTextSize.pointSize))
                    .foregroundStyle(ATMTheme.primary)
                    .lineSpacing(2)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            .padding(.bottom, isLast ? 0 : 12)
            Spacer(minLength: 0)
        }
    }

    private func entryMarkerColor(_ entry: ATMTodoProgressEntry) -> Color {
        if entry.isDoneMarker { return ATMTheme.success }
        // 补充与普通进展都用 accent：原来是 Color.blue，和 accent 几乎同色但不跟随
        // 系统强调色；「补充」的区分本来就靠那枚标签在承担，不靠时间轴上的点。
        return ATMTheme.accent
    }
}
