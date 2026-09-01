import Foundation

/// One CLI and whether it is actually gated.
///
/// Decoded from `atm guard status --json`. The two problem states are separate
/// fields rather than one "broken" flag because they are different problems with
/// different fixes, and both are silent: nothing fails, no command errors, and the
/// user goes on believing sends are being reviewed.
struct ATMGuardTool: Decodable, Identifiable, Equatable {
    let tool: String
    /// Empty when the tool is not on PATH and no path was ever recorded — normal
    /// for one only ever invoked by absolute path, and the case that needs a path
    /// typed in before it can be gated at all.
    let binPath: String
    let realPath: String?
    let installed: Bool
    /// False when the recorded path is gone — usually a CLI that moved or was
    /// uninstalled. Distinguished from "not gated" because the fix is different and
    /// because otherwise the only way to find out is to press the button.
    let binExists: Bool?
    /// Something overwrote the shim, usually the tool upgrading itself.
    let clobbered: Bool
    /// PATH finds a different copy first, so invocations by bare name never reach
    /// the gate.
    let shadowedBy: String?
    let rules: Int

    var id: String { tool }

    enum CodingKeys: String, CodingKey {
        case tool, installed, clobbered, rules
        case binExists = "bin_exists"
        case binPath = "bin_path"
        case realPath = "real_path"
        case shadowedBy = "shadowed_by"
    }

    var isShadowed: Bool { !(shadowedBy ?? "").isEmpty }

    /// Needs a path typed in before anything can be done with it.
    var needsPath: Bool { binPath.isEmpty }

    /// A path was recorded but nothing is there any more.
    var pathIsMissing: Bool { !binPath.isEmpty && binExists == false }

    /// Installed, not overwritten, not bypassed, and has something to match.
    var isHealthy: Bool { installed && !clobbered && !isShadowed && rules > 0 }

    /// Whether the card should offer a path field: nothing recorded, the recorded
    /// path is gone, or it is simply not gated yet.
    var needsPathInput: Bool { needsPath || pathIsMissing || !installed }

    /// The one-line state, in the order the problems matter.
    var stateText: String {
        if needsPath { return "未找到路径" }
        if pathIsMissing { return "路径不存在" }
        if clobbered && installed { return "真身丢了" }
        if clobbered { return "被覆盖" }
        if !installed { return "未启用" }
        if isShadowed { return "被 PATH 绕过" }
        if rules == 0 { return "无规则，全部直通" }
        return "已启用"
    }

    /// What to do about it, or nil when there is nothing to say. Deliberately
    /// concrete: a warning without a next step is a warning people learn to ignore.
    var problemAdvice: String? {
        if needsPath { return "这个 CLI 不在 PATH 上，填它的绝对路径再启用" }
        if pathIsMissing {
            return "记录的这个路径上已经没有文件了（CLI 被移动或卸载过？）。填新路径再启用"
        }
        if clobbered && installed { return "被移开的真身不见了，闸门无法执行命令；先把它放回去" }
        if clobbered { return "闸门被覆盖了（通常是这个 CLI 自己升级），重新启用可修复" }
        if installed && isShadowed {
            return "PATH 会先找到 \(shadowedBy ?? "")，按名字调用不经过闸门；改成启用那一份"
        }
        if installed && rules == 0 { return "没有启用的规则，这个 CLI 的调用会全部直通" }
        return nil
    }
}

/// One rule, as the settings UI needs to see it.
///
/// Provenance is carried because switching a built-in off and deleting a rule you
/// wrote are different actions: a built-in comes back if its override is removed,
/// so "remove" is the wrong verb for it.
struct ATMGuardRule: Decodable, Identifiable, Equatable {
    let tool: String
    let ruleID: String
    let label: String?
    let path: [String]?
    let argvPattern: String?
    let targetFlags: [String]?
    let bodyFlags: [String]?
    let enabled: Bool
    let builtin: Bool
    let overridden: Bool

    var id: String { "\(tool)/\(ruleID)" }

    enum CodingKeys: String, CodingKey {
        case tool, label, path, enabled, builtin, overridden
        case ruleID = "id"
        case argvPattern = "argv_pattern"
        case targetFlags = "target_flags"
        case bodyFlags = "body_flags"
    }

    /// Which commands this rule is about, in the form it was written.
    var matcherText: String {
        var parts: [String] = []
        if let path, !path.isEmpty { parts.append(path.joined(separator: " ")) }
        if let argvPattern, !argvPattern.isEmpty { parts.append("~ \(argvPattern)") }
        return parts.joined(separator: " · ")
    }

    var originText: String {
        if builtin { return overridden ? "内置 · 已改" : "内置" }
        return "自定义"
    }

    /// A built-in can be switched off but not deleted: removing its override
    /// restores it, which is not what someone who wants it gone means.
    var isDeletable: Bool { !builtin }
}

/// A rule being composed in the settings form, and the JSON the CLI expects.
///
/// The form asks for a subcommand path and, optionally, which flags carry the
/// target and the message. Extractors are optional on purpose: a rule with none
/// still gates, and the approval card falls back to showing the whole command —
/// which is worse to read but never wrong.
struct ATMGuardRuleDraft: Equatable {
    var tool: String = ""
    var ruleID: String = ""
    var label: String = ""
    /// Space-separated, as the command is typed: `chat message send`.
    var path: String = ""
    /// Comma-separated flag names: `--group,--user`.
    var targetFlags: String = ""
    var bodyFlags: String = ""

    private static func tokens(_ value: String, separators: CharacterSet) -> [String] {
        value.components(separatedBy: separators)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    var pathTokens: [String] { Self.tokens(path, separators: .whitespaces) }
    var targetFlagTokens: [String] { Self.tokens(targetFlags, separators: CharacterSet(charactersIn: ",")) }
    var bodyFlagTokens: [String] { Self.tokens(bodyFlags, separators: CharacterSet(charactersIn: ",")) }

    /// Why this cannot be saved yet, or nil when it can.
    ///
    /// A path is required rather than optional: a rule with no matcher is a patch
    /// onto a built-in, and the CLI refuses one for an id nothing ships — better to
    /// say so in the form than to surface that as an error afterwards.
    var validationMessage: String? {
        if tool.trimmingCharacters(in: .whitespaces).isEmpty { return "填一个 CLI 名字" }
        if ruleID.trimmingCharacters(in: .whitespaces).isEmpty { return "填一个规则 id" }
        if pathTokens.isEmpty { return "填要拦的子命令，比如 chat message send" }
        return nil
    }

    /// The rule object `atm guard rule set` reads from stdin.
    func jsonPayload() throws -> Data {
        var rule: [String: Any] = [
            "id": ruleID.trimmingCharacters(in: .whitespaces),
            "path": pathTokens,
        ]
        let trimmedLabel = label.trimmingCharacters(in: .whitespaces)
        if !trimmedLabel.isEmpty { rule["label"] = trimmedLabel }
        if !targetFlagTokens.isEmpty { rule["target"] = ["flags": targetFlagTokens] }
        if !bodyFlagTokens.isEmpty { rule["body"] = ["flags": bodyFlagTokens] }
        return try JSONSerialization.data(withJSONObject: rule)
    }

    /// The payload for switching an existing rule on or off: id plus the flag, and
    /// deliberately nothing else, so a built-in's matcher is never restated and
    /// cannot drift from the real one.
    static func togglePayload(ruleID: String, enabled: Bool) throws -> Data {
        try JSONSerialization.data(withJSONObject: ["id": ruleID, "enabled": enabled])
    }
}
