import os
import XCTest

final class DataPlaneLogLevelTests: XCTestCase {
    /// Warnings explain why a connection was cut, usually hours after the
    /// fact, so they must land at a level the unified log keeps on disk.
    func testWarningsAndErrorsUsePersistedLogTypes() {
        XCTAssertEqual(DataPlaneLogLevel.logType(for: "warn"), .default)
        XCTAssertEqual(DataPlaneLogLevel.logType(for: "error"), .error)
    }

    func testUnknownLevelIsNotDowngraded() {
        XCTAssertEqual(DataPlaneLogLevel.logType(for: "fatal"), .error)
        XCTAssertEqual(DataPlaneLogLevel.logType(for: ""), .error)
    }
}
