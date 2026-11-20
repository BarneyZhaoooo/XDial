struct NetworkChangeNoticeBaseline {
    struct Path: Equatable {
        let available: Bool
        let usesWiFi: Bool
        let interfaces: [String]
        let gateways: [String]
    }

    private var lastPath: Path?
    private var lastSSID: String?

    mutating func changed(path: Path, ssid: String?) -> Bool {
        let previousPath = lastPath
        let previousSSID = lastSSID
        lastPath = path
        // A missing SSID can mean location access is unavailable. Compare
        // names only within the same Wi-Fi path, across readable samples.
        lastSSID = path.usesWiFi ? (ssid ?? (path == previousPath ? previousSSID : nil)) : nil
        guard let previousPath else { return false }
        if previousPath != path { return true }
        guard path.usesWiFi, let previousSSID, let ssid else { return false }
        return ssid != previousSSID
    }
}
