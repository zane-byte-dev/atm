import AppKit
import XCTest
@testable import ATMMenuBarApp

final class ATMImagePasteMonitorTests: XCTestCase {
    func testPlainCommandVIsTheOnlyInterceptedPasteShortcut() throws {
        XCTAssertTrue(ATMImagePasteShortcut.matches(try event(modifiers: [.command])))
        XCTAssertTrue(ATMImagePasteShortcut.matches(try event(modifiers: [.command, .capsLock])))
        XCTAssertFalse(ATMImagePasteShortcut.matches(try event(modifiers: [])))
        XCTAssertFalse(ATMImagePasteShortcut.matches(try event(modifiers: [.command, .shift])))
        XCTAssertFalse(ATMImagePasteShortcut.matches(try event(modifiers: [.command, .option, .shift])))
    }

    func testOtherCommandKeyIsNotTreatedAsPaste() throws {
        XCTAssertFalse(ATMImagePasteShortcut.matches(try event(modifiers: [.command], characters: "c")))
    }

    private func event(
        modifiers: NSEvent.ModifierFlags,
        characters: String = "v"
    ) throws -> NSEvent {
        try XCTUnwrap(NSEvent.keyEvent(
            with: .keyDown,
            location: .zero,
            modifierFlags: modifiers,
            timestamp: 0,
            windowNumber: 1,
            context: nil,
            characters: characters,
            charactersIgnoringModifiers: characters,
            isARepeat: false,
            keyCode: 9
        ))
    }
}
