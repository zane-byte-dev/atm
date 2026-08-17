import AppKit
import XCTest
@testable import ATMMenuBarApp

/// The task list and the quick panel share one todo right-click menu. Before that,
/// each had hand-written its own subset, so the actions a row offered depended on
/// where you right-clicked it — and the rows without an icon sat indented with a
/// gap where their icon would be, which is what made the menu look ragged.
@MainActor
final class ATMTodoMenuTests: XCTestCase {
    private func makeTodo(
        id: String = "t1",
        status: String = "open",
        links: [(title: String, url: String)] = []
    ) throws -> ATMTodo {
        var fields = [
            "\"id\":\"\(id)\"",
            "\"title\":\"\(id)\"",
            "\"priority\":\"P1\"",
            "\"status\":\"\(status)\"",
            "\"created\":\"2026-08-06\"",
            "\"project\":\"atm\"",
        ]
        if !links.isEmpty {
            let encoded = links
                .map { "{\"title\":\"\($0.title)\",\"url\":\"\($0.url)\"}" }
                .joined(separator: ",")
            fields.append("\"links\":[\(encoded)]")
        }
        return try JSONDecoder().decode(
            ATMTodo.self,
            from: Data("{\(fields.joined(separator: ","))}".utf8)
        )
    }

    private func menu(
        for todo: ATMTodo,
        isTrashed: Bool = false,
        onEdit: (() -> Void)? = nil,
        onPermanentDelete: (() -> Void)? = nil
    ) -> NSMenu {
        ATMRightClickMenu.make(
            ATMTodoMenu.entries(
                for: todo,
                store: ATMDataStore(),
                isTrashed: isTrashed,
                onEdit: onEdit,
                onPermanentDelete: onPermanentDelete
            )
        )
    }

    /// Handing a todo to an agent is what a row is mostly for, so the launch
    /// prompt leads the menu instead of sitting near the bottom.
    func testLaunchPromptLeadsTheMenu() throws {
        let items = menu(for: try makeTodo()).items
        XCTAssertEqual(items.first?.title, "复制启动提示")
    }

    /// AppKit indents every title to clear the widest image in the menu, so one
    /// icon-less row does not sit flush left — it sits indented next to a hole.
    func testEveryItemCarriesAnIcon() throws {
        let todo = try makeTodo(links: [(title: "MR", url: "https://example.com")])
        for menu in [menu(for: todo, onEdit: {}), menu(for: todo, isTrashed: true, onPermanentDelete: {})] {
            for item in menu.items where !item.isSeparatorItem {
                XCTAssertNotNil(item.image, "「\(item.title)」缺 icon，整个菜单的标题会跟着错开")
                for child in item.submenu?.items ?? [] {
                    XCTAssertNotNil(child.image, "子菜单「\(child.title)」缺 icon")
                }
            }
        }
    }

    /// Every lifecycle transition the detail pane can perform is on the row too:
    /// the task list used to be the only place with them, and the quick panel had
    /// none at all.
    func testMenuCoversEveryLifecycleActionPlusTheUtilities() throws {
        let todo = try makeTodo()
        let titles = menu(for: todo, onEdit: {}).items.map(\.title)

        for item in ATMTodoStatusActions.items(for: todo) {
            XCTAssertTrue(titles.contains(item.title), "缺少生命周期操作「\(item.title)」")
        }
        XCTAssertTrue(titles.contains("优化任务"))
        XCTAssertTrue(titles.contains("编辑任务…"))
        XCTAssertTrue(titles.contains("复制 ID"))
        XCTAssertTrue(titles.contains("用 VS Code 打开项目"))
        XCTAssertEqual(titles.last, "移到回收站", "破坏性操作留在最后一格")
    }

    /// The quick panel has no edit form, so it passes no callback and must not get
    /// a dead menu row.
    func testEditRowOnlyAppearsWhenTheCallerCanEdit() throws {
        let todo = try makeTodo()
        XCTAssertFalse(menu(for: todo).items.map(\.title).contains("编辑任务…"))
        XCTAssertTrue(menu(for: todo, onEdit: {}).items.map(\.title).contains("编辑任务…"))
    }

    /// Review is the human gate: there is nothing to hand to an agent there.
    func testReviewTodoHidesTheLaunchPrompt() throws {
        let titles = menu(for: try makeTodo(status: "review")).items.map(\.title)
        XCTAssertFalse(titles.contains("复制启动提示"))
    }

    func testClosedTodoHidesTheLaunchPrompt() throws {
        for status in ["done", "dropped"] {
            let titles = menu(for: try makeTodo(status: status)).items.map(\.title)
            XCTAssertFalse(titles.contains("复制启动提示"))
            XCTAssertFalse(titles.contains("优化任务"))
        }
    }

    func testLinksBecomeASubmenuOnlyWhenThereAreLinks() throws {
        let plain = menu(for: try makeTodo()).items.map(\.title)
        XCTAssertFalse(plain.contains("打开链接"))

        let linked = menu(for: try makeTodo(links: [
            (title: "MR", url: "https://example.com/mr"),
            (title: "CR", url: "https://example.com/cr"),
        ]))
        let submenu = try XCTUnwrap(linked.items.first { $0.title == "打开链接" }?.submenu)
        XCTAssertEqual(submenu.items.map(\.title), ["MR", "CR"])
    }

    /// A trashed row has its own two actions; offering 开始 or 移到回收站 there would
    /// act on a todo that is already out of the working set.
    func testTrashedRowOffersRestoreAndPermanentDeleteOnly() throws {
        let items = menu(for: try makeTodo(), isTrashed: true, onPermanentDelete: {}).items
        XCTAssertEqual(
            items.filter { !$0.isSeparatorItem }.map(\.title),
            ["恢复", "复制 ID", "永久删除…"]
        )
    }

    func testPermanentDeleteIsRedAndCallsBack() throws {
        var deleted = 0
        let menu = menu(for: try makeTodo(), isTrashed: true, onPermanentDelete: { deleted += 1 })
        let item = try XCTUnwrap(menu.items.first { $0.title == "永久删除…" })

        let color = item.attributedTitle?.attribute(
            .foregroundColor,
            at: 0,
            effectiveRange: nil
        ) as? NSColor
        XCTAssertEqual(color, .systemRed)

        try XCTUnwrap(item.target as? ATMContextMenuAction).invoke()
        XCTAssertEqual(deleted, 1)
    }
}
