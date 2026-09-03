import AppKit
import SwiftUI

struct DesktopTodoLinksView: View {
    let todo: ATMTodo
    @ObservedObject var store: ATMDataStore
    let isArchived: Bool

    @State private var editor: LinkEditorSelection?
    @State private var removing: ATMTodoLink?
    @State private var isRemoving = false
    @State private var errorMessage: String?

    private struct LinkEditorSelection: Identifiable {
        let id = UUID()
        let link: ATMTodoLink?
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(alignment: .top) {
                Text(isArchived ? "已归档 · 关联内容只读" : "集中查看任务涉及的变更、发布、文档与交付入口。")
                    .font(ATMFont.footnote)
                    .foregroundStyle(ATMTheme.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if !isArchived {
                    Button { editor = LinkEditorSelection(link: nil) } label: {
                        Label("添加关联", systemImage: "plus")
                    }
                    .disabled(store.isActing || isRemoving)
                }
            }

            if let errorMessage {
                Text(errorMessage).font(ATMFont.footnote).foregroundStyle(.red).textSelection(.enabled)
            }

            if todo.links?.isEmpty != false {
                VStack(spacing: 10) {
                    Image(systemName: "link").font(.system(size: 28)).foregroundStyle(ATMTheme.secondary)
                    Text("暂无关联内容").font(ATMFont.font(.body, weight: .medium))
                    Text("CR、MR、发布单、访问地址和文档都可以关联在这里。")
                        .font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity).padding(.vertical, 36)
            } else {
                ForEach(ATMTodoLinkGroup.allCases) { group in
                    let links = (todo.links ?? []).filter { $0.group == group }
                    if !links.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            Text("\(group.rawValue) · \(links.count)")
                                .font(ATMFont.font(.body, weight: .semibold))
                            ForEach(links, id: \.url) { link in linkRow(link) }
                        }
                    }
                }
                Text("关联仅提供上下文与入口，不代表依赖、合并、发布或验收已完成。")
                    .font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
            }
        }
        .padding(.horizontal, ATMDetailLayout.horizontalPadding)
        .padding(.vertical, 16)
        .frame(maxWidth: ATMDetailLayout.contentMaxWidth, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .sheet(item: $editor) { selection in
            TodoLinkEditor(todoID: todo.id, link: selection.link, store: store)
        }
        .alert("解除关联？", isPresented: Binding(
            get: { removing != nil }, set: { if !$0 { removing = nil } }
        ), presenting: removing) { link in
            Button("取消", role: .cancel) { removing = nil }
            Button("解除关联", role: .destructive) { remove(link) }
        } message: { link in
            Text("仅从此任务移除“\(link.displayTitle)”，不会删除外部内容。")
        }
    }

    private func linkRow(_ link: ATMTodoLink) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .top, spacing: 8) {
                VStack(alignment: .leading, spacing: 5) {
                    Text(link.kindLabel).font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
                    if let destination = link.destination {
                        Link(destination: destination) {
                            Label(link.displayTitle, systemImage: "arrow.up.right.square")
                                .font(ATMFont.font(.body, weight: .medium))
                                .lineLimit(2)
                        }
                    } else {
                        Text(link.displayTitle).font(ATMFont.font(.body, weight: .medium))
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(link.url, forType: .string)
                } label: { Image(systemName: "doc.on.doc") }
                    .buttonStyle(.borderless).help("复制地址").accessibilityLabel("复制地址")
                if !isArchived {
                    Menu {
                        Button("编辑关联") { editor = LinkEditorSelection(link: link) }
                        Button("解除关联", role: .destructive) { removing = link }
                    } label: { Image(systemName: "ellipsis") }
                    .menuStyle(.borderlessButton).fixedSize()
                    .disabled(store.isActing || isRemoving)
                    .accessibilityLabel("关联操作")
                }
            }
            Text(link.url).font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
                .lineLimit(2).truncationMode(.middle).textSelection(.enabled)
            if let relation = link.relationLabel {
                Text("用途：\(relation)").font(ATMFont.footnote).textSelection(.enabled)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ATMTheme.controlFill, in: RoundedRectangle(cornerRadius: 10))
    }

    private func remove(_ link: ATMTodoLink) {
        isRemoving = true
        errorMessage = nil
        Task { @MainActor in
            defer { isRemoving = false }
            do { try await store.removeTodoLink(todoID: todo.id, url: link.url) }
            catch { errorMessage = error.localizedDescription }
        }
    }
}

private struct TodoLinkEditor: View {
    let todoID: String
    let link: ATMTodoLink?
    @ObservedObject var store: ATMDataStore
    @Environment(\.dismiss) private var dismiss
    @State private var url: String
    @State private var title: String
    @State private var kind: String
    @State private var relation: String
    @State private var isSaving = false
    @State private var errorMessage: String?

    init(todoID: String, link: ATMTodoLink?, store: ATMDataStore) {
        self.todoID = todoID
        self.link = link
        self.store = store
        _url = State(initialValue: link?.url ?? "")
        _title = State(initialValue: link?.title ?? "")
        _kind = State(initialValue: link?.kind ?? "")
        _relation = State(initialValue: link?.relation ?? "")
    }

    private var validURL: Bool {
        ATMTodoLink(url: url.trimmingCharacters(in: .whitespacesAndNewlines), kind: nil, title: nil, relation: nil).destination != nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(link == nil ? "添加关联" : "编辑关联").font(.headline)
            Form {
                TextField("地址", text: $url, prompt: Text("https://…"))
                TextField("标题", text: $title, prompt: Text("选填，便于辨认"))
                Picker("类型", selection: $kind) {
                    ForEach(ATMTodoLink.kindOptions, id: \.value) { option in
                        Text(option.title).tag(option.value)
                    }
                    if !ATMTodoLink.kindOptions.contains(where: { $0.value == kind }) {
                        Text(kind).tag(kind)
                    }
                }
                TextField("用途", text: $relation, prompt: Text("选填，例如：验收证据、部署入口"))
            }
            .disabled(isSaving)
            if kind.isEmpty {
                Text("识别结果：\(ATMTodoLink(url: url, kind: nil, title: nil, relation: nil).kindLabel) · 可手动修改类型")
                    .font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
            }
            Text("仅支持 HTTP / HTTPS 地址，请勿粘贴包含令牌、密码或临时签名的链接。")
                .font(ATMFont.footnote).foregroundStyle(ATMTheme.secondary)
            if let errorMessage {
                Text(errorMessage).font(ATMFont.footnote).foregroundStyle(.red).textSelection(.enabled)
            }
            HStack {
                Spacer()
                if isSaving { ProgressView().controlSize(.small) }
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction).disabled(isSaving)
                Button("保存") { save() }.buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction).disabled(!validURL || isSaving || store.isActing)
            }
        }
        .padding(24).frame(width: 480)
        .interactiveDismissDisabled(isSaving)
    }

    private func save() {
        isSaving = true
        errorMessage = nil
        Task { @MainActor in
            defer { isSaving = false }
            do {
                try await store.saveTodoLink(ATMTodoLinkSaveRequest(
                    todoID: todoID, originalURL: link?.url, url: url,
                    kind: kind, title: title, relation: relation
                ))
                dismiss()
            } catch { errorMessage = error.localizedDescription }
        }
    }
}
