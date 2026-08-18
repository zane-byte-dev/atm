import Foundation

/// One outbound action an agent tried to take, waiting on a decision.
///
/// Decoded from `atm guard list --json`. Only the fields this UI actually renders
/// are listed: the CLI is the record, and a column the panel never shows is a
/// column the panel should not claim to know about.
struct ATMGuardApproval: Decodable, Identifiable, Equatable {
    let id: String
    let tool: String
    let label: String?
    let previewTarget: String?
    let previewTitle: String?
    let previewBody: String?
    let status: String
    /// What a reader should believe now. A pending request past its expiry reads
    /// as expired here while still stored as pending, because the CLI computes
    /// this without a write.
    let effectiveStatus: String?
    let cwd: String?
    let envAgent: String?
    let attachCount: Int?
    let requestedAt: Int64
    let expiresAt: Int64
    let reason: String?
    let ranBy: String?
    let exitCode: Int?
    let output: String?

    enum CodingKeys: String, CodingKey {
        case id, tool, label, status, cwd, reason, output
        case previewTarget = "preview_target"
        case previewTitle = "preview_title"
        case previewBody = "preview_body"
        case effectiveStatus = "effective_status"
        case envAgent = "env_agent"
        case attachCount = "attach_count"
        case requestedAt = "requested_at"
        case expiresAt = "expires_at"
        case ranBy = "ran_by"
        case exitCode = "exit_code"
    }

    /// What the row is, in one line: the kind of action and who it reaches.
    var actionLine: String {
        let name = (label?.isEmpty == false ? label! : tool)
        guard let target = previewTarget, !target.isEmpty else { return name }
        return "\(name) → \(target)"
    }

    var state: String { effectiveStatus ?? status }

    var isPending: Bool { state == "pending" }

    /// A request that started executing and never reported back. Nothing may
    /// retry it — whether the action took effect is not recorded anywhere — so the
    /// UI's whole job here is to say so.
    var isExecutingWithUnknownOutcome: Bool { state == "running" }
}

/// The wire shape pushed over the notch socket the moment a request is created,
/// so a banner appears immediately instead of at the next poll.
struct ATMGuardRequest: Decodable, Equatable {
    let version: Int
    let id: String
    let tool: String
    let label: String?
    let target: String?
    let title: String?
    let body: String?
    let cwd: String?
    let agent: String?
    let expiresAt: Int64?

    enum CodingKeys: String, CodingKey {
        case version = "v"
        case id, tool, label, target, title, body, cwd, agent
        case expiresAt = "expires_at"
    }

    /// Matches the CLI's own version gate: an envelope from a newer build might
    /// mean something different by the same field names.
    var isSupported: Bool { version <= 1 }
}

/// Identifiers for the two buttons on the banner. Kept as constants because the
/// same strings have to appear in the registered category and in the delegate that
/// reads the response back.
enum ATMGuardApprovalActions {
    static let category = "ATM_GUARD_APPROVAL"
    static let approve = "ATM_GUARD_APPROVE"
    static let deny = "ATM_GUARD_DENY"
}

/// Banner copy for a pending request.
///
/// A pure value so the wording is testable without a notification centre — which
/// matters more here than elsewhere, because this banner carries two buttons that
/// send a real message when pressed.
struct ATMGuardApprovalPayload: Equatable {
    let title: String
    let subtitle: String
    let body: String

    static func make(request: ATMGuardRequest) -> ATMGuardApprovalPayload {
        make(
            tool: request.tool,
            label: request.label,
            target: request.target,
            messageTitle: request.title,
            messageBody: request.body
        )
    }

    static func make(approval: ATMGuardApproval) -> ATMGuardApprovalPayload {
        make(
            tool: approval.tool,
            label: approval.label,
            target: approval.previewTarget,
            messageTitle: approval.previewTitle,
            messageBody: approval.previewBody
        )
    }

    private static func make(
        tool: String,
        label: String?,
        target: String?,
        messageTitle: String?,
        messageBody: String?
    ) -> ATMGuardApprovalPayload {
        let action = (label?.isEmpty == false ? label! : tool)
        var subtitle = action
        if let target, !target.isEmpty {
            subtitle = "\(action) → \(target)"
        }
        // The message itself is the body: approving sends it, so the one thing the
        // user must see before pressing a button is what would go out. The title
        // is prefixed rather than dropped when both exist.
        var body = messageBody?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if let messageTitle, !messageTitle.isEmpty {
            body = body.isEmpty ? messageTitle : "\(messageTitle)\n\(body)"
        }
        if body.isEmpty {
            body = "批准后 ATM 会执行这条命令"
        }
        return ATMGuardApprovalPayload(title: "ATM · 待授权", subtitle: subtitle, body: body)
    }
}

/// Decides which banners to raise and which to pull back, from a polled list.
///
/// Separated from the store for the same reason the agent-attention tracker is: a
/// banner that lingers after the request is already decided is worse than never
/// having sent it, and that is a rule worth testing without a clock or a
/// subprocess.
struct ATMGuardApprovalNotifyDiff: Equatable {
    var post: [ATMGuardApproval] = []
    var withdraw: [String] = []

    /// `notified` is the set of ids a banner has already been raised for. Passing
    /// nil means "first load": nothing is posted, so launching ATM with a pile of
    /// pending requests does not produce a pile of banners.
    static func next(
        notified: Set<String>?,
        approvals: [ATMGuardApproval]
    ) -> (diff: ATMGuardApprovalNotifyDiff, notified: Set<String>) {
        let pending = approvals.filter(\.isPending)
        let pendingIDs = Set(pending.map(\.id))
        guard let notified else {
            return (ATMGuardApprovalNotifyDiff(), pendingIDs)
        }
        var diff = ATMGuardApprovalNotifyDiff()
        diff.post = pending.filter { !notified.contains($0.id) }
        // Withdraw anything that was notified and is no longer pending, whether it
        // was decided here, decided in a terminal, or quietly expired.
        diff.withdraw = notified.subtracting(pendingIDs).sorted()
        return (diff, pendingIDs)
    }
}
