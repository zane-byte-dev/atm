import AppKit
import XCTest
@testable import ATMMenuBarApp

/// Every right-click menu in the app is built through `ATMRightClickMenu` and
/// popped from an AppKit overlay, so the row underneath keeps its own selection
/// appearance. These menus are shown outside the responder chain: an item whose
/// target got collected, or a menu left on AppKit's auto-enabling, silently does
/// nothing when clicked.
final class ATMRightClickMenuTests: XCTestCase {
    func testEntriesBecomeItemsInOrderWithSeparators() {
        let menu = ATMRightClickMenu.make {
            ATMMenuItem("查看聊天记录") {}
            ATMMenuSeparator()
            ATMMenuItem("删除记录", destructive: true) {}
        }

        XCTAssertEqual(menu.items.map(\.title), ["查看聊天记录", "", "删除记录"])
        XCTAssertTrue(menu.items[1].isSeparatorItem)
    }

    func testActionItemsKeepAnExplicitTargetAndAreEnabled() throws {
        let menu = ATMRightClickMenu.make { ATMMenuItem("编辑") {} }
        let item = try XCTUnwrap(menu.items.first)

        XCTAssertFalse(menu.autoenablesItems, "自动 enable 会盖掉 enabled: 参数")
        XCTAssertEqual(item.action, #selector(ATMContextMenuAction.invoke))
        XCTAssertNotNil(item.target)
        XCTAssertTrue(item.isEnabled)
    }

    /// The action object has no other owner: if `representedObject` stopped
    /// holding it, the target would be gone by the time the menu is shown.
    func testClickingAnItemRunsItsHandler() throws {
        var clicked = 0
        let menu = ATMRightClickMenu.make { ATMMenuItem("暂停") { clicked += 1 } }

        let item = try XCTUnwrap(menu.items.first)
        let target = try XCTUnwrap(item.target as? ATMContextMenuAction)
        XCTAssertTrue(item.representedObject as AnyObject === target)
        target.invoke()

        XCTAssertEqual(clicked, 1)
    }

    func testDestructiveItemIsRedAndPlainItemIsNot() {
        let menu = ATMRightClickMenu.make {
            ATMMenuItem("删除…", destructive: true) {}
            ATMMenuItem("复制 ID") {}
        }

        let color = menu.items[0].attributedTitle?.attribute(
            .foregroundColor,
            at: 0,
            effectiveRange: nil
        ) as? NSColor
        XCTAssertEqual(color, .systemRed)
        XCTAssertNil(menu.items[1].attributedTitle)
    }

    func testDisabledItemStaysDisabled() {
        let menu = ATMRightClickMenu.make {
            ATMMenuItem("没有其他知识库", enabled: false) {}
        }

        XCTAssertFalse(menu.items[0].isEnabled)
    }

    func testSubmenuCarriesItsOwnEntriesAndEnablement() throws {
        let menu = ATMRightClickMenu.make {
            ATMMenuSubmenu("移动到", systemImage: "folder") {
                ATMMenuItem("收件箱") {}
                ATMMenuItem("工作") {}
            }
        }

        let item = try XCTUnwrap(menu.items.first)
        XCTAssertTrue(item.isEnabled)
        // AppKit wires its own `submenuAction:` / target once a submenu is
        // attached; what matters is that the parent row carries no handler of ours.
        XCTAssertNil(item.representedObject, "父项只展开子菜单，本身不该有动作")
        XCTAssertFalse(item.target is ATMContextMenuAction)
        XCTAssertEqual(item.submenu?.items.map(\.title), ["收件箱", "工作"])
        XCTAssertEqual(item.submenu?.autoenablesItems, false)
    }

    /// `if` / `for` support is what let the `.contextMenu` call sites convert
    /// without restructuring — a builder that dropped them would quietly lose rows.
    func testBuilderSupportsConditionalsAndLoops() {
        @ATMMenuBuilder
        func entries(showsPrompt: Bool, links: [String]) -> [ATMMenuEntry] {
            ATMMenuItem("打开") {}
            if showsPrompt {
                ATMMenuItem("复制启动提示") {}
            }
            for link in links {
                ATMMenuItem(link) {}
            }
        }

        XCTAssertEqual(
            ATMRightClickMenu.make(entries(showsPrompt: false, links: [])).items.map(\.title),
            ["打开"]
        )
        XCTAssertEqual(
            ATMRightClickMenu.make(entries(showsPrompt: true, links: ["a", "b"])).items.map(\.title),
            ["打开", "复制启动提示", "a", "b"]
        )
    }

    /// An empty menu must stay empty: the overlay skips popping one, which is how
    /// a row with no applicable actions right-clicks to nothing instead of a stub.
    func testEmptyEntriesProduceAnEmptyMenu() {
        XCTAssertTrue(ATMRightClickMenu.make {}.items.isEmpty)
    }
}
