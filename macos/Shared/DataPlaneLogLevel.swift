import os

/// Maps a data-plane log level onto a unified-log type that is kept on disk.
///
/// `.info` and `.debug` live in memory only, so a warning recorded at either
/// would be gone by the time someone asks why a connection was cut.
enum DataPlaneLogLevel {
    static func logType(for level: String) -> OSLogType {
        level == "warn" ? .default : .error
    }
}
