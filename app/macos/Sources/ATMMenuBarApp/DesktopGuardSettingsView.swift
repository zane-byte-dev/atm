import AppKit
import SwiftUI

/// Registers and manages the CLIs the outbound action gate stands in front of.
///
/// Lives in settings rather than a workspace: this is configuration you touch when
/// something changes, not something to watch. The pending requests themselves are
/// a glance-and-act moment and live in the quick panel.
struct DesktopGuardSettingsView: View {
    @ObservedObject var store: ATMDataStore

    @State private var draft = ATMGuardRuleDraft()
    @State private var isAddingRule = false
    @State private var pathEdits: [String: String] = [:]
    @State private var confirmingUninstall: ATMGuardTool?
    @State private var confirmingRemoveRule: ATMGuardRule?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                card { intro }
                ForEach(store.guardTools) { tool in
                    card { toolSection(tool) }
                }
                card { addRuleSection }
            }
            .padding(20)
        }
        .onAppear { store.loadGuardConfiguration() }
        .confirmationDialog(
            "撤掉 \(confirmingUninstall?.tool ?? "") 的闸门？",
            isPresented: .constant(confirmingUninstall != nil),
            titleVisibility: .visible
        ) {
            Button("撤掉，把二进制放回原位", role: .destructive) {
                if let tool = confirmingUninstall { store.uninstallGuardTool(tool.tool) }
                confirmingUninstall = nil
            }
            Button("取消", role: .cancel) { confirmingUninstall = nil }
        } message: {
            Text("撤掉之后这个 CLI 的外发动作不再需要你批准，Agent 调它就直接发出去了。")
        }
        .confirmationDialog(
            "删掉规则 \(confirmingRemoveRule?.ruleID ?? "")？",
            isPresented: .constant(confirmingRemoveRule != nil),
            titleVisibility: .visible
        ) {
            Button("删掉", role: .destructive) {
                if let rule = confirmingRemoveRule { store.removeGuardRule(rule) }
                confirmingRemoveRule = nil
            }
            Button("取消", role: .cancel) { confirmingRemoveRule = nil }
        } message: {
            Text("删掉之后这条命令不再被拦。想临时停掉、以后还能开回来的话，用左边的开关。")
        }
    }

    // MARK: - Intro

    private var intro: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("外发动作闸门")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text(
                        "启用之后，ATM 会挡在这个 CLI 的二进制前面：读操作原样直通，"
                            + "命中规则的外发动作先来问你，批准后由 ATM 自己执行。"
                            + "每台机器上的所有 Agent 都会经过它，不用逐个配置。"
                    )
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                if store.isUpdatingGuardConfig {
                    ProgressView().controlSize(.small)
                }
            }
            Text("拦不到通过 MCP 工具完成的外发动作——那不经过命令执行。")
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
            // Only errors not attributable to one tool land here; the rest are shown
            // in that tool's own section, next to the button that produced them.
            if store.guardConfigErrorTool == nil, let error = store.guardConfigErrorMessage {
                Text(error)
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            HStack(spacing: 10) {
                Button("重新检测") { store.loadGuardConfiguration() }
                Spacer()
            }
            .controlSize(.small)
            .disabled(store.isUpdatingGuardConfig)
        }
    }

    // MARK: - One tool

    private func toolSection(_ tool: ATMGuardTool) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 10) {
                Circle()
                    .fill(stateColor(tool))
                    .frame(width: 7, height: 7)
                    .padding(.top, 6)
                VStack(alignment: .leading, spacing: 3) {
                    Text(tool.tool)
                        .font(ATMFont.font(.body, weight: .semibold))
                    Text(tool.stateText)
                        .font(ATMFont.footnote)
                        .foregroundStyle(stateColor(tool))
                    if !tool.binPath.isEmpty {
                        Text(tool.binPath)
                            .font(ATMFont.mono(.micro))
                            .foregroundStyle(ATMTheme.secondary)
                            .textSelection(.enabled)
                    }
                }
                Spacer(minLength: 12)
                toolActions(tool)
            }

            if let advice = tool.problemAdvice {
                Label(advice, systemImage: "exclamationmark.triangle.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.warning)
                    .fixedSize(horizontal: false, vertical: true)
            }

            // Next to the button that was pressed. The same text at the top of the
            // pane reads as "nothing happened", which is exactly how a failed 启用
            // used to look.
            if store.guardConfigErrorTool == tool.tool, let error = store.guardConfigErrorMessage {
                Label(error, systemImage: "xmark.octagon.fill")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.danger)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }

            // A CLI that is not on PATH, or whose recorded path has gone, cannot be
            // gated until somebody says where it is. This is the only way `dws` ever
            // gets a gate.
            if tool.needsPathInput {
                pathField(tool)
            }

            let rules = store.guardRules.filter { $0.tool == tool.tool }
            if rules.isEmpty {
                Text("还没有规则。下面加一条，否则这个 CLI 的调用会全部直通。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(rules) { rule in
                        ruleRow(rule)
                    }
                }
            }
        }
    }

    private func toolActions(_ tool: ATMGuardTool) -> some View {
        HStack(spacing: 8) {
            if store.guardToolInFlight == tool.tool {
                ProgressView().controlSize(.small)
            }
            if tool.installed {
                // Reinstall is the repair for an overwritten shim, so it is offered
                // as such rather than hidden behind uninstall-then-install.
                if tool.clobbered {
                    Button("修复") { store.installGuardTool(tool.tool, bin: tool.binPath) }
                        .buttonStyle(.borderedProminent)
                }
                Button("撤掉") { confirmingUninstall = tool }
            } else if !tool.needsPath && !tool.pathIsMissing {
                Button("启用") { store.installGuardTool(tool.tool, bin: tool.binPath) }
                    .buttonStyle(.borderedProminent)
            }
            if !tool.installed && tool.rules > 0 {
                Menu {
                    Button("忘掉这个 CLI") { store.forgetGuardTool(tool.tool) }
                } label: {
                    Image(systemName: "ellipsis")
                }
                .menuIndicator(.hidden)
                .fixedSize()
            }
        }
        .controlSize(.small)
        .disabled(store.isUpdatingGuardConfig)
    }

    private func pathField(_ tool: ATMGuardTool) -> some View {
        HStack(spacing: 8) {
            TextField(
                "这个 CLI 的绝对路径",
                text: Binding(
                    // A path that no longer exists is not offered as a starting
                    // point: re-submitting it is the one thing that cannot work.
                    get: { pathEdits[tool.tool] ?? (tool.pathIsMissing ? "" : tool.binPath) },
                    set: { pathEdits[tool.tool] = $0 }
                )
            )
            .textFieldStyle(.roundedBorder)
            .font(ATMFont.mono(.caption))
            Button("选择…") { chooseBinary(for: tool) }
            Button("启用") {
                let path = effectivePath(tool)
                guard !path.isEmpty else { return }
                store.installGuardTool(tool.tool, bin: path)
            }
            .buttonStyle(.borderedProminent)
            .disabled(effectivePath(tool).isEmpty)
        }
        .controlSize(.small)
        .disabled(store.isUpdatingGuardConfig)
    }

    private func effectivePath(_ tool: ATMGuardTool) -> String {
        (pathEdits[tool.tool] ?? (tool.pathIsMissing ? "" : tool.binPath))
            .trimmingCharacters(in: .whitespaces)
    }

    private func chooseBinary(for tool: ATMGuardTool) {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        // These live in dot-directories, which the panel hides by default.
        panel.showsHiddenFiles = true
        panel.message = "选择 \(tool.tool) 的可执行文件"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        pathEdits[tool.tool] = url.path
    }

    // MARK: - One rule

    private func ruleRow(_ rule: ATMGuardRule) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Toggle(
                "",
                isOn: Binding(
                    get: { rule.enabled },
                    set: { store.setGuardRuleEnabled(rule, enabled: $0) }
                )
            )
            .labelsHidden()
            .toggleStyle(.switch)
            .controlSize(.mini)
            .disabled(store.isUpdatingGuardConfig)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(rule.label?.isEmpty == false ? rule.label! : rule.ruleID)
                        .font(ATMFont.font(.caption, weight: .medium))
                        .foregroundStyle(rule.enabled ? ATMTheme.primary : ATMTheme.secondary)
                    Text(rule.originText)
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                        .padding(.horizontal, 5)
                        .background(ATMTheme.controlFill, in: Capsule())
                }
                Text(rule.matcherText)
                    .font(ATMFont.mono(.micro))
                    .foregroundStyle(ATMTheme.secondary)
                    .textSelection(.enabled)
                if let flags = previewFlagsText(rule) {
                    Text(flags)
                        .font(ATMFont.mono(.micro))
                        .foregroundStyle(ATMTheme.secondary)
                }
            }
            Spacer(minLength: 8)
            // A built-in has no delete: removing its override would restore it, which
            // is not what deleting means.
            if rule.isDeletable {
                Button {
                    confirmingRemoveRule = rule
                } label: {
                    Image(systemName: "trash")
                }
                .buttonStyle(.borderless)
                .controlSize(.small)
                .disabled(store.isUpdatingGuardConfig)
            }
        }
        .padding(9)
        .background(ATMTheme.controlFill.opacity(rule.enabled ? 1 : 0.5),
                    in: RoundedRectangle(cornerRadius: 8))
    }

    private func previewFlagsText(_ rule: ATMGuardRule) -> String? {
        var parts: [String] = []
        if let target = rule.targetFlags, !target.isEmpty {
            parts.append("接收方 " + target.joined(separator: " / "))
        }
        if let body = rule.bodyFlags, !body.isEmpty {
            parts.append("正文 " + body.joined(separator: " / "))
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    // MARK: - Add a rule

    private var addRuleSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("加一条规则")
                        .font(ATMFont.font(.bodyLarge, weight: .semibold))
                    Text(
                        "填 CLI 名字和要拦的子命令。填过的 CLI 会自动出现在上面，"
                            + "补上路径就能启用。"
                    )
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 12)
                if !isAddingRule {
                    Button("加规则") { isAddingRule = true }
                        .controlSize(.small)
                }
            }

            if isAddingRule {
                VStack(alignment: .leading, spacing: 8) {
                    labeledField("CLI 名字", "dws", $draft.tool, mono: true)
                    labeledField("规则 id", "chat-send", $draft.ruleID, mono: true)
                    labeledField("说明", "发送钉钉消息", $draft.label, mono: false)
                    labeledField("要拦的子命令", "chat message send", $draft.path, mono: true)
                    labeledField("接收方参数（可选）", "--group,--user", $draft.targetFlags, mono: true)
                    labeledField("正文参数（可选）", "--text", $draft.bodyFlags, mono: true)

                    Text(
                        "接收方和正文参数只影响待授权卡片显示什么；不填也照样拦，"
                            + "卡片会退化成显示整条命令。"
                    )
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .fixedSize(horizontal: false, vertical: true)

                    if let problem = draft.validationMessage {
                        Text(problem)
                            .font(ATMFont.footnote)
                            .foregroundStyle(ATMTheme.warning)
                    }

                    HStack(spacing: 10) {
                        Button("保存") {
                            store.saveGuardRule(draft)
                            draft = ATMGuardRuleDraft()
                            isAddingRule = false
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(draft.validationMessage != nil || store.isUpdatingGuardConfig)
                        Button("取消") {
                            draft = ATMGuardRuleDraft()
                            isAddingRule = false
                        }
                        Spacer()
                    }
                    .controlSize(.small)
                }
            }
        }
    }

    private func labeledField(
        _ label: String, _ placeholder: String, _ text: Binding<String>, mono: Bool
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label)
                .font(ATMFont.footnote)
                .foregroundStyle(ATMTheme.secondary)
                .frame(width: 132, alignment: .trailing)
            TextField(placeholder, text: text)
                .textFieldStyle(.roundedBorder)
                .font(mono ? ATMFont.mono(.caption) : ATMFont.font(.caption))
        }
        .controlSize(.small)
    }

    private func stateColor(_ tool: ATMGuardTool) -> Color {
        if tool.isHealthy { return ATMTheme.success }
        if tool.installed || tool.clobbered { return ATMTheme.warning }
        return ATMTheme.secondary
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .padding(18)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .bottom) {
                Rectangle()
                    .fill(ATMTheme.border.opacity(0.72))
                    .frame(height: 1)
            }
    }
}
