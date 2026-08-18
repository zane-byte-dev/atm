import AppKit
import SwiftUI

/// The window that asks whether an outbound action may go out.
///
/// Its own window rather than a section of the menu-bar panel, because a menu-bar
/// panel is a *transient* surface: it closes the moment it loses focus, and it is
/// anchored to a place you have to go looking at. This decision has a deadline and
/// an outward effect, so it has to stay put until it is answered.
///
/// It is a non-activating floating panel, which is the compromise that makes an
/// unbidden window acceptable: it appears over whatever you are doing and follows
/// you across Spaces, but it never takes keyboard focus, so it cannot swallow what
/// you were typing.
@MainActor
final class ATMApprovalPresenter {
    private let store: ATMDataStore
    private var panel: FloatingPanel?
    /// Requests already shown, so re-polling the same pending list does not keep
    /// re-opening a window the user deliberately dismissed.
    private var presentedIDs: Set<String> = []

    init(store: ATMDataStore) {
        self.store = store
    }

    /// Shows the window for newly arrived requests, and closes it once nothing is
    /// pending. `arrived` is the ids that are new this round; the window itself
    /// always renders every pending request, so a second one that lands while the
    /// first is still up joins the same window instead of stacking another.
    func present(arrived: [ATMGuardApproval], pending: [ATMGuardApproval]) {
        guard !pending.isEmpty else {
            presentedIDs.removeAll()
            close()
            return
        }
        let unseen = arrived.filter { !presentedIDs.contains($0.id) }
        presentedIDs.formUnion(pending.map(\.id))
        // Drop ids that are no longer pending, so the same command asking again
        // later does get a window.
        presentedIDs.formIntersection(Set(pending.map(\.id)))

        if panel?.isVisible == true {
            // Already up: the view observes the store, so it repaints on its own.
            return
        }
        guard !unseen.isEmpty else { return }
        show()
    }

    /// Opens the window on purpose, for a request that was missed.
    func openManually() {
        guard !store.pendingApprovals.filter(\.isPending).isEmpty else { return }
        show()
    }

    private func show() {
        let panel = panel ?? makePanel()
        self.panel = panel
        panel.host(
            ApprovalRequestView(
                store: store,
                dismiss: { [weak self] in self?.close() }
            )
        )
        position(panel)
        // orderFrontRegardless, not makeKey: showing up must not steal focus from
        // whatever the user is typing in.
        panel.orderFrontRegardless()
        ATMAgentSoundPlayer.shared.play(.attentionRequired)
    }

    private func close() {
        panel?.orderOut(nil)
    }

    private func makePanel() -> FloatingPanel {
        let panel = FloatingPanel(size: NSSize(width: 420, height: 380))
        panel.minSize = NSSize(width: 380, height: 240)
        // Dismissing without deciding is allowed: the request stays pending and the
        // quick panel still lists it. Refusing to close would be worse than a
        // decision made just to get rid of a window.
        panel.onDismiss = { [weak panel] in panel?.orderOut(nil) }
        return panel
    }

    /// Top-right, under the menu bar: near where notifications appear, and out of
    /// the middle of whatever is being read.
    private func position(_ panel: FloatingPanel) {
        guard let screen = NSScreen.main else { return }
        let visible = screen.visibleFrame
        let margin: CGFloat = 16
        panel.setFrameOrigin(
            NSPoint(
                x: visible.maxX - panel.frame.width - margin,
                y: visible.maxY - panel.frame.height - margin
            ))
    }
}

/// The contents of the approval window.
struct ApprovalRequestView: View {
    @ObservedObject var store: ATMDataStore
    let dismiss: () -> Void

    private var pending: [ATMGuardApproval] {
        store.pendingApprovals.filter(\.isPending)
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            if pending.isEmpty {
                emptyState
            } else {
                ScrollView(showsIndicators: false) {
                    VStack(spacing: 12) {
                        ForEach(pending) { approval in
                            requestCard(approval)
                        }
                    }
                    .padding(14)
                }
            }
        }
        .background(Color.clear)
        .frame(minWidth: 380, minHeight: 240)
        .atmHidesScrollBars()
    }

    private var header: some View {
        HStack(spacing: 8) {
            Image(systemName: "hand.raised.fill")
                .foregroundStyle(ATMTheme.danger)
            Text(pending.count > 1 ? "待授权外发 · \(pending.count)" : "待授权外发")
                .font(ATMFont.font(.body, weight: .semibold))
                .foregroundStyle(ATMTheme.primary)
            Spacer(minLength: 8)
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
            }
            .buttonStyle(.borderless)
            .controlSize(.small)
            // Says what closing does, because "did I just approve it?" is the one
            // thing a user must never have to wonder about here.
            .help("先放着不处理。请求还在，随后可以从菜单栏面板处理")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
    }

    private var emptyState: some View {
        VStack(spacing: 6) {
            Text("没有待授权的外发动作")
                .font(ATMFont.font(.caption, weight: .medium))
                .foregroundStyle(ATMTheme.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func requestCard(_ approval: ATMGuardApproval) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                Text(approval.label?.isEmpty == false ? approval.label! : approval.tool)
                    .font(ATMFont.font(.body, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                if let target = approval.previewTarget, !target.isEmpty {
                    Text("发给 \(target)")
                        .font(ATMFont.font(.caption))
                        .foregroundStyle(ATMTheme.secondary)
                        .textSelection(.enabled)
                }
            }

            // The message in full, scrollable rather than truncated: approving sends
            // exactly this, and a decision about text you cannot finish reading is
            // not a decision.
            if let title = approval.previewTitle, !title.isEmpty {
                Text(title)
                    .font(ATMFont.font(.caption, weight: .semibold))
                    .foregroundStyle(ATMTheme.primary)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            if let body = approval.previewBody?.trimmingCharacters(in: .whitespacesAndNewlines),
               !body.isEmpty {
                ScrollView(showsIndicators: false) {
                    Text(body)
                        .font(ATMFont.font(.caption))
                        .foregroundStyle(ATMTheme.primary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
                .frame(maxHeight: 150)
                .padding(9)
                .background(ATMTheme.surface, in: RoundedRectangle(cornerRadius: 8))
            }

            HStack(spacing: 8) {
                if let source = approval.envAgent, !source.isEmpty {
                    Text(source)
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                }
                if let cwd = approval.cwd, !cwd.isEmpty {
                    Text(shortPath(cwd))
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                        .lineLimit(1)
                        .truncationMode(.head)
                }
                Spacer(minLength: 4)
                if let attempts = approval.attachCount, attempts > 1 {
                    // Worth showing: it means the agent kept retrying, i.e. it is
                    // waiting on this rather than having moved on.
                    Text("重试 \(attempts - 1) 次")
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.warning)
                }
            }

            HStack(spacing: 8) {
                Button("批准并发送") { store.decideApproval(approval, approve: true) }
                    .buttonStyle(.borderedProminent)
                Button("拒绝") { store.decideApproval(approval, approve: false) }
                    .buttonStyle(.bordered)
                Spacer(minLength: 0)
            }
            .controlSize(.regular)
            .disabled(store.isDecidingApproval(approval.id))

            if let error = store.approvalErrorMessage {
                Text(error)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.elevated, in: RoundedRectangle(cornerRadius: 10))
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(ATMTheme.danger.opacity(0.25), lineWidth: 1)
        )
    }

    private func shortPath(_ path: String) -> String {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        return path.hasPrefix(home) ? "~" + path.dropFirst(home.count) : path
    }
}
