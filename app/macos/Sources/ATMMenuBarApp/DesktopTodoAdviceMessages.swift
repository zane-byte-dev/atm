import AppKit
import SwiftUI

/// Advice is delivered in the task's message band, across every detail tab.
/// The conclusion is visible immediately; only supporting evidence is folded.
struct DesktopTodoAdviceMessages: View {
    let todoID: String
    @ObservedObject var store: ATMDataStore

    private var loading: Bool { store.loadingAdviceTodoIDs.contains(todoID) }
    private var result: ATMTodoAdviceResponse? { store.adviceByTodoID[todoID] }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let result, !result.reviews.isEmpty {
                HStack(spacing: 6) {
                    Label("建议", systemImage: "lightbulb")
                        .font(ATMFont.font(.caption, weight: .semibold))
                    if let date = result.checkedDate {
                        Text(date.formatted(date: .omitted, time: .shortened))
                            .font(ATMFont.caption)
                    }
                    Spacer()
                    if loading {
                        ProgressView().controlSize(.mini)
                    }
                    Button { store.loadAdvice(for: todoID, force: true) } label: {
                        Label("刷新", systemImage: "arrow.clockwise")
                    }
                    .font(ATMFont.caption)
                    .buttonStyle(.plain)
                    .disabled(loading)
                }
                .foregroundStyle(ATMTheme.secondary)
                ForEach(result.reviews) { review in
                    ATMInlineNotice(
                        severity: review.severity == "warning" || !review.errors.isEmpty ? .warning : .info,
                        title: review.messageTitle,
                        message: review.suggestion,
                        details: review.evidence(checkedAt: result.checkedAt),
                        actionTitle: "打开 CR",
                        actionSystemImage: "arrow.up.right.square",
                        onAction: {
                            if let url = URL(string: review.url) { NSWorkspace.shared.open(url) }
                        }
                    )
                }
            }
            if let error = store.adviceErrorByTodoID[todoID] {
                ATMInlineNotice(
                    severity: .warning, title: "建议获取失败", message: error,
                    actionTitle: "重试", isActionEnabled: !loading,
                    onAction: { store.loadAdvice(for: todoID, force: true) }
                )
            }
        }
    }
}
