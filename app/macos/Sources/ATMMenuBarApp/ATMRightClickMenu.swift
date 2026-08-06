import AppKit
import SwiftUI

/// Right-click menus in ATM are built as `NSMenu` and popped from an overlay that
/// only accepts right-mouse hits, never with SwiftUI's `.contextMenu`.
///
/// `.contextMenu` paints an accent-colored box around the row for as long as the
/// menu is open, so right-clicking a collection record *looked* like it moved the
/// selection while the detail pane kept showing the row that was actually
/// selected. Popping the menu from an AppKit overlay leaves the row — and the real
/// selection — untouched.
///
/// Call sites use `ATMMenuItem` / `ATMMenuSeparator` / `ATMMenuSubmenu` inside an
/// `atmRightClickMenu { }` block, which reads like the `Button` / `Divider` /
/// `Menu` they replaced:
///
///     .atmRightClickMenu {
///         ATMMenuItem("打开 Todo") { openTodo(item) }
///         ATMMenuSeparator()
///         ATMMenuItem("删除记录", destructive: true) { deleteItemCandidate = item }
///     }
///
/// One row of such a menu, after the call site's value types are resolved.
enum ATMMenuEntry {
    case separator
    case action(
        title: String,
        systemImage: String?,
        isDestructive: Bool,
        isEnabled: Bool,
        handler: () -> Void
    )
    case submenu(title: String, systemImage: String?, entries: [ATMMenuEntry])
}

protocol ATMMenuEntryConvertible {
    var menuEntry: ATMMenuEntry { get }
}

struct ATMMenuItem: ATMMenuEntryConvertible {
    let menuEntry: ATMMenuEntry

    /// `destructive` only colors the title red: AppKit has no destructive item
    /// style, and a red title is what the task-list menu already used.
    init(
        _ title: String,
        systemImage: String? = nil,
        destructive: Bool = false,
        enabled: Bool = true,
        action: @escaping () -> Void
    ) {
        menuEntry = .action(
            title: title,
            systemImage: systemImage,
            isDestructive: destructive,
            isEnabled: enabled,
            handler: action
        )
    }
}

struct ATMMenuSeparator: ATMMenuEntryConvertible {
    let menuEntry: ATMMenuEntry = .separator

    init() {}
}

struct ATMMenuSubmenu: ATMMenuEntryConvertible {
    let menuEntry: ATMMenuEntry

    init(
        _ title: String,
        systemImage: String? = nil,
        @ATMMenuBuilder entries: () -> [ATMMenuEntry]
    ) {
        menuEntry = .submenu(title: title, systemImage: systemImage, entries: entries())
    }
}

/// Supports `if` / `for` / `switch` the way `.contextMenu` did, so converting a
/// menu is a change of spelling rather than a restructuring. Helper functions that
/// return `[ATMMenuEntry]` splice in as-is.
@resultBuilder
enum ATMMenuBuilder {
    static func buildBlock(_ parts: [ATMMenuEntry]...) -> [ATMMenuEntry] { parts.flatMap { $0 } }
    static func buildExpression(_ expression: ATMMenuEntryConvertible) -> [ATMMenuEntry] {
        [expression.menuEntry]
    }
    static func buildExpression(_ expression: [ATMMenuEntry]) -> [ATMMenuEntry] { expression }
    static func buildOptional(_ part: [ATMMenuEntry]?) -> [ATMMenuEntry] { part ?? [] }
    static func buildEither(first part: [ATMMenuEntry]) -> [ATMMenuEntry] { part }
    static func buildEither(second part: [ATMMenuEntry]) -> [ATMMenuEntry] { part }
    static func buildArray(_ parts: [[ATMMenuEntry]]) -> [ATMMenuEntry] { parts.flatMap { $0 } }
}

enum ATMRightClickMenu {
    static func make(@ATMMenuBuilder _ entries: () -> [ATMMenuEntry]) -> NSMenu {
        make(entries())
    }

    static func make(_ entries: [ATMMenuEntry]) -> NSMenu {
        let menu = NSMenu()
        // Explicit enablement: these menus are popped outside the responder chain,
        // so AppKit's auto-enabling has no validator to consult and would just
        // overwrite whatever `enabled:` a call site asked for.
        menu.autoenablesItems = false
        for entry in entries {
            menu.addItem(makeItem(entry))
        }
        return menu
    }

    private static func makeItem(_ entry: ATMMenuEntry) -> NSMenuItem {
        switch entry {
        case .separator:
            return .separator()

        case let .action(title, systemImage, isDestructive, isEnabled, handler):
            let target = ATMContextMenuAction(handler)
            let item = NSMenuItem(
                title: title,
                action: #selector(ATMContextMenuAction.invoke),
                keyEquivalent: ""
            )
            item.target = target
            // The action object has no other owner — `representedObject` keeps it
            // alive as long as the menu is, so the target is still there on click.
            item.representedObject = target
            item.isEnabled = isEnabled
            if isDestructive {
                item.attributedTitle = NSAttributedString(
                    string: title,
                    attributes: [.foregroundColor: NSColor.systemRed]
                )
            }
            if let systemImage {
                item.image = NSImage(systemSymbolName: systemImage, accessibilityDescription: nil)
            }
            return item

        case let .submenu(title, systemImage, entries):
            let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
            let submenu = make(entries)
            submenu.title = title
            item.submenu = submenu
            item.isEnabled = true
            if let systemImage {
                item.image = NSImage(systemSymbolName: systemImage, accessibilityDescription: nil)
            }
            return item
        }
    }
}

final class ATMContextMenuAction: NSObject {
    private let handler: () -> Void

    init(_ handler: @escaping () -> Void) {
        self.handler = handler
    }

    @objc func invoke() {
        handler()
    }
}

private final class ATMRightClickMenuView: NSView {
    var makeMenu: () -> NSMenu = { NSMenu() }

    /// Invisible to left clicks: the row underneath keeps its own button, hover,
    /// and drag behaviour. Only a right-click lands here.
    override func hitTest(_ point: NSPoint) -> NSView? {
        switch NSApp.currentEvent?.type {
        case .rightMouseDown, .rightMouseUp:
            return self
        default:
            return nil
        }
    }

    override func rightMouseDown(with event: NSEvent) {
        let menu = makeMenu()
        guard !menu.items.isEmpty else { return }
        NSMenu.popUpContextMenu(menu, with: event, for: self)
    }
}

private struct ATMRightClickMenuHost: NSViewRepresentable {
    let makeMenu: () -> NSMenu

    func makeNSView(context: Context) -> ATMRightClickMenuView {
        let view = ATMRightClickMenuView()
        view.makeMenu = makeMenu
        return view
    }

    func updateNSView(_ nsView: ATMRightClickMenuView, context: Context) {
        nsView.makeMenu = makeMenu
    }
}

extension View {
    /// Drop-in replacement for `.contextMenu` that leaves the row's selection
    /// appearance alone. Entries are rebuilt on each right-click, so they see
    /// current state.
    func atmRightClickMenu(@ATMMenuBuilder _ entries: @escaping () -> [ATMMenuEntry]) -> some View {
        overlay {
            ATMRightClickMenuHost { ATMRightClickMenu.make(entries()) }
                .accessibilityHidden(true)
        }
    }
}
