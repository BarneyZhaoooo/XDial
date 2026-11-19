import Foundation
import UserNotifications

enum ConnectionNotice: String {
    case disconnect = "xdial.notify.disconnect"
    case scenario = "xdial.notify.scenario"

    static var preferences: UserDefaults {
        #if os(iOS)
        UserDefaults(suiteName: XDialIdentity.appGroup)!
        #else
        xdialDefaults
        #endif
    }

    func post(scenarioName: String = "") {
        let defaults = Self.preferences
        guard defaults.bool(forKey: rawValue) else { return }
        let language = defaults.string(forKey: "xdial.language") ?? Locale.preferredLanguages.first
        let chinese = language?.hasPrefix("zh") == true
        let content = UNMutableNotificationContent()
        switch self {
        case .disconnect:
            content.title = chinese ? "XDial 意外断线" : "XDial disconnected unexpectedly"
        case .scenario:
            content.title = chinese ? "XDial 场景已切换" : "XDial switched scenarios"
            content.body = scenarioName
        }
        let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request) { error in
            if let error { NSLog("XDial notification failed: %@", error.localizedDescription) }
        }
    }
}
