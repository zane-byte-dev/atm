import AppKit

/// The one right-click menu for a todo row, shared by the task list and the quick
/// panel. Both had grown their own hand-written subsets — the quick panel offered
/// two items and no lifecycle action at all, the task list offered lifecycle
/// actions but no way to edit — so which actions a row had depended on where you
/// right-clicked it.
///
/// Ordering rule: what you reach for most sits at the top, above the first
/// separator. Copying the launch prompt is the whole point of a row for an
/// agent-driven workflow, so it leads; lifecycle transitions follow; the
/// destructive action stays alone at the bottom.
///
/// Every item carries an icon. AppKit indents titles to clear the widest image in
/// the menu, so a single icon-less row does not sit flush left — it sits indented
/// with a gap where its icon would be, which is what made the old menu look
/// ragged.
@MainActor
enum ATMTodoMenu {
    @ATMMenuBuilder
    static func entries(
        for todo: ATMTodo,
        store: ATMDataStore,
        isTrashed: Bool = false,
        onEdit: (() -> Void)? = nil,
        onPermanentDelete: (() -> Void)? = nil
    ) -> [ATMMenuEntry] {
        if isTrashed {
            ATMMenuItem("恢复", systemImage: "arrow.uturn.backward") {
                store.perform(.restore, on: todo)
            }
            copyIDItem(for: todo)
            if let onPermanentDelete {
                ATMMenuSeparator()
                ATMMenuItem("永久删除…", systemImage: "trash.slash", destructive: true) {
                    onPermanentDelete()
                }
            }
        } else {
            if ATMTodoStatusActions.showsLaunchPrompt(for: todo) {
                ATMMenuItem("复制启动提示", systemImage: "doc.on.doc") {
                    Task {
                        guard let prompt = await store.launchPrompt(for: todo) else { return }
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(prompt, forType: .string)
                    }
                }
            }
            ATMMenuItem(
                "用 VS Code 打开项目",
                systemImage: "chevron.left.forwardslash.chevron.right"
            ) {
                store.openTodoProjectInVSCode(todo)
            }

            ATMMenuSeparator()
            for item in ATMTodoStatusActions.items(for: todo) {
                ATMMenuItem(item.title, systemImage: item.systemImage) {
                    store.perform(item.action, on: todo)
                }
            }

            ATMMenuSeparator()
            if let onEdit {
                ATMMenuItem("编辑任务…", systemImage: "pencil") { onEdit() }
            }
            if let links = todo.links, !links.isEmpty {
                ATMMenuSubmenu("打开链接", systemImage: "link") {
                    for link in links {
                        ATMMenuItem(
                            link.title ?? link.url,
                            systemImage: "arrow.up.right.square"
                        ) {
                            guard let url = URL(string: link.url) else { return }
                            NSWorkspace.shared.open(url)
                        }
                    }
                }
            }
            copyIDItem(for: todo)

            ATMMenuSeparator()
            ATMMenuItem("移到回收站", systemImage: "trash") {
                store.perform(.trash, on: todo)
            }
        }
    }

    private static func copyIDItem(for todo: ATMTodo) -> [ATMMenuEntry] {
        [
            ATMMenuItem("复制 ID", systemImage: "number") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(todo.id, forType: .string)
            }.menuEntry,
        ]
    }
}
