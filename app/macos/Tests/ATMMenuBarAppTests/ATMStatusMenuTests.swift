import AppKit
import XCTest
@testable import ATMMenuBarApp

/// The status item's right-click menu is the only add-task entry that works while
/// the quick panel is closed, and it is built outside any window — so an item with
/// a missing target silently does nothing rather than falling back to the
/// responder chain.
final class ATMStatusMenuTests: XCTestCase {
    private final class Target {}

    func testMenuOffersAddTodoWiredToTheController() {
        let target = Target()
        let menu = ATMStatusMenu.make(target: target)

        XCTAssertEqual(
            menu.items.map(\.title),
            ["打开 ATM 主窗口", "添加任务", "打开快速面板", "同步并刷新", "", "退出 ATM"]
        )

        let add = menu.item(withTitle: "添加任务")
        XCTAssertEqual(add?.action, #selector(StatusBarController.addTodoFromMenu))
        XCTAssertEqual(add?.keyEquivalent, "n")
    }

    func testEveryActionableItemKeepsAnExplicitTarget() {
        let target = Target()
        let menu = ATMStatusMenu.make(target: target)

        for item in menu.items where !item.isSeparatorItem {
            XCTAssertNotNil(item.action, "\(item.title) 没有绑定 action")
            XCTAssertTrue(item.target === target, "\(item.title) 没有绑定到状态栏控制器")
        }
    }
}
