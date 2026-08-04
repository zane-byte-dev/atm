import SwiftUI

/// Reads one agent session's full Q/A and shows it. Every place that lists
/// sessions — global search, a todo's bound sessions — eventually needs "what
/// was actually said in there", so the sheet owns its own loading instead of
/// making each caller carry transcript/loading/error state.
struct ATMSessionTranscriptSheet: View {
    @Environment(\.dismiss) private var dismiss

    let agent: String
    /// The session's topic when the caller knows it; the header falls back to
    /// agent + project, which is all global search has.
    let title: String?
    let shortID: String
    let project: String
    @ObservedObject var store: ATMDataStore

    @State private var transcript = ""
    @State private var errorMessage: String?
    @State private var isLoading = true

    private var headline: String {
        if let title = title?.trimmingCharacters(in: .whitespacesAndNewlines), !title.isEmpty {
            return title
        }
        let name = ATMAgentDisplay.name(agent)
        return project.isEmpty ? name : "\(name) · \(project)"
    }

    private var subtitle: String {
        var parts = [shortID]
        if title != nil {
            parts.append(ATMAgentDisplay.name(agent))
            if !project.isEmpty { parts.append(project) }
        }
        return parts.joined(separator: " · ")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                ATMAgentMark(agent: agent, size: 22)
                VStack(alignment: .leading, spacing: 3) {
                    Text(headline)
                        .font(ATMFont.font(.title3, weight: .semibold))
                        .lineLimit(2)
                    Text(subtitle)
                        .font(ATMFont.mono(.footnote))
                        .foregroundStyle(ATMTheme.secondary)
                }
                Spacer()
                Button("关闭") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(16)
            .background(ATMTheme.surface)
            Divider()
            Group {
                if isLoading {
                    VStack(spacing: 10) {
                        ProgressView()
                        Text("正在读取完整会话")
                            .font(ATMFont.body)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let errorMessage {
                    VStack(spacing: 10) {
                        Image(systemName: "exclamationmark.triangle")
                            .font(ATMFont.font(.display, weight: .light))
                        Text("无法读取会话").font(ATMFont.font(.title3, weight: .semibold))
                        Text(errorMessage)
                            .font(ATMFont.body)
                            .foregroundStyle(ATMTheme.secondary)
                    }
                    .padding(28)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ScrollView {
                        Text(transcript.isEmpty ? "该会话暂无可展示内容。" : transcript)
                            .font(ATMFont.mono(.body))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(18)
                    }
                }
            }
            .background(ATMTheme.canvas)
        }
        .frame(minWidth: 720, minHeight: 600)
        .task { await load() }
    }

    private func load() async {
        isLoading = true
        errorMessage = nil
        do {
            transcript = try await store.sessionTranscript(shortID)
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}
