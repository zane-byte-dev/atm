import Foundation

/// A discoverable inventory of ATM's fixed application shortcuts. Configurable
/// global bindings are rendered separately in Settings because they carry live
/// enablement, registration and recorder state; this catalog covers the remaining
/// menu, editor and contextual commands.
struct ATMShortcutReference: Identifiable, Equatable {
    let id: String
    let title: String
    let detail: String
    let keys: String
}

struct ATMShortcutGroup: Identifiable, Equatable {
    let id: String
    let title: String
    let detail: String
    let shortcuts: [ATMShortcutReference]
}

enum ATMShortcutCatalog {
    static let groups: [ATMShortcutGroup] = [
        ATMShortcutGroup(
            id: "actions",
            title: "常用操作",
            detail: "这些操作在对应页面或编辑状态下生效。",
            shortcuts: [
                shortcut("new-todo", "新建任务", "在任意页面打开新建任务卡片。", "⌘N"),
                shortcut("search", "全局搜索", "搜索任务、会话、知识与共享记忆。", "⌘K"),
                shortcut("save-knowledge", "保存知识编辑", "保存当前知识文档的修改。", "⌘S"),
                shortcut("submit-form", "提交表单", "保存任务修改或提交当前表单。", "⌘↩"),
                shortcut("cancel", "取消", "关闭弹窗、退出编辑或取消语音输入。", "⎋"),
            ]
        ),
        ATMShortcutGroup(
            id: "navigation",
            title: "浏览与窗口",
            detail: "分区、页面历史和 macOS 窗口操作。",
            shortcuts: [
                shortcut("section", "切换分区", "按侧栏顺序：任务、收集、Agent、知识、用量、设置。", "⌘1–⌘6"),
                shortcut("back", "后退", "返回上一个浏览位置。", "⌘["),
                shortcut("forward", "前进", "前往下一个浏览位置。", "⌘]"),
                shortcut("minimize", "最小化窗口", "把当前窗口缩到程序坞。", "⌘M"),
                shortcut("close", "关闭窗口", "关闭当前窗口，ATM 仍在菜单栏运行。", "⌘W"),
                shortcut("hide", "隐藏 ATM", "隐藏 ATM 的所有窗口。", "⌘H"),
                shortcut("hide-others", "隐藏其他应用", "只保留 ATM 可见。", "⌥⌘H"),
                shortcut("quit", "退出 ATM", "退出应用和菜单栏进程。", "⌘Q"),
            ]
        ),
        ATMShortcutGroup(
            id: "editing",
            title: "文本编辑",
            detail: "适用于任务、知识、搜索等可编辑文本区域。",
            shortcuts: [
                shortcut("undo", "撤销", "撤销上一步文本修改。", "⌘Z"),
                shortcut("redo", "重做", "恢复刚撤销的修改。", "⇧⌘Z"),
                shortcut("cut", "剪切", "剪切选中的文本。", "⌘X"),
                shortcut("copy", "复制", "复制选中的文本。", "⌘C"),
                shortcut("paste", "粘贴", "粘贴剪贴板内容。", "⌘V"),
                shortcut("paste-plain", "粘贴为纯文本", "粘贴内容并去掉原格式。", "⌥⇧⌘V"),
                shortcut("select-all", "全选", "选择当前编辑区的全部内容。", "⌘A"),
            ]
        ),
        ATMShortcutGroup(
            id: "search-results",
            title: "搜索结果",
            detail: "焦点位于顶部搜索框时生效，也兼容中文输入法候选。",
            shortcuts: [
                shortcut("search-move", "切换结果", "在搜索结果间上下移动。", "↑ / ↓"),
                shortcut("search-open", "打开结果", "打开当前选中的搜索结果。", "↩"),
                shortcut("search-close", "关闭结果", "收起搜索结果下拉框。", "⎋"),
            ]
        ),
    ]

    static var allShortcuts: [ATMShortcutReference] {
        groups.flatMap(\.shortcuts)
    }

    private static func shortcut(
        _ id: String,
        _ title: String,
        _ detail: String,
        _ keys: String
    ) -> ATMShortcutReference {
        ATMShortcutReference(id: id, title: title, detail: detail, keys: keys)
    }
}
