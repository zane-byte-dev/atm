import XCTest
@testable import ATMMenuBarApp

final class CollectionIntervalInputTests: XCTestCase {
    func testConvertsSupportedUnitsToMinutes() {
        XCTAssertEqual(CollectionIntervalInput(text: "15", unit: .minute).minutes, 15)
        XCTAssertEqual(CollectionIntervalInput(text: "6", unit: .hour).minutes, 360)
        XCTAssertEqual(CollectionIntervalInput(text: "1", unit: .day).minutes, 1440)
    }

    func testChoosesReadableUnitForStoredMinutes() {
        let minutes = CollectionIntervalInput.displayValue(for: 15)
        XCTAssertEqual(minutes.text, "15")
        XCTAssertEqual(minutes.unit, .minute)

        let hours = CollectionIntervalInput.displayValue(for: 360)
        XCTAssertEqual(hours.text, "6")
        XCTAssertEqual(hours.unit, .hour)

        let day = CollectionIntervalInput.displayValue(for: 1440)
        XCTAssertEqual(day.text, "1")
        XCTAssertEqual(day.unit, .day)
    }

    func testRejectsMissingNonIntegerAndNonPositiveValues() {
        XCTAssertEqual(
            CollectionIntervalInput(text: "", unit: .minute).validationMessage,
            "请输入采集频率"
        )
        XCTAssertEqual(
            CollectionIntervalInput(text: "1.5", unit: .hour).validationMessage,
            "采集频率必须是正整数"
        )
        XCTAssertEqual(
            CollectionIntervalInput(text: "0", unit: .minute).validationMessage,
            "采集频率必须是正整数"
        )
    }

    func testRejectsIntervalsLongerThanOneDay() {
        XCTAssertEqual(
            CollectionIntervalInput(text: "25", unit: .hour).validationMessage,
            "采集间隔不能超过 1 天"
        )
        XCTAssertEqual(
            CollectionIntervalInput(text: "2", unit: .day).validationMessage,
            "采集间隔不能超过 1 天"
        )
    }
}
