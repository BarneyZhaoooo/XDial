import Foundation

/// Compile-time product identity shared by the host, helper-facing code,
/// Settings carrier, and Network Extension. There are exactly two: Debug
/// defines XDIAL_DEVELOPMENT_IDENTITY; FormalDevelopment and Release use the
/// established production identity, which beta releases share unchanged.
enum XDialBuildIdentity {
    /// Names the installation identity, not the update channel: a beta is a
    /// formal build and must report "stable" so its Tailscale node keeps the
    /// formal device name.
    static var tailscaleDeviceChannel: String {
        #if XDIAL_DEVELOPMENT_IDENTITY
        return "debug"
        #else
        return "stable"
        #endif
    }

    static let formalApplicationIdentifier = "com.xd.xdial.app"
    static let formalHelperIdentifier = "com.xd.xdial.app.helper"
    static let formalDaemonIdentifier = "com.xd.xdial.app.daemon"
    static let formalTransparentProxyIdentifier =
        "com.xd.xdial.app.transparent-proxy"
    static let formalSettingsUIIdentifier =
        "com.xd.xdial.app.settings-ui"
    static let formalDataIdentifier = "com.kafeifei.xdial"
    static let formalUserDataDirectoryName = ".xdial"
    static let developmentApplicationIdentifier =
        "com.xd.xdial.debug"
    static let developmentSettingsUIIdentifier =
        "com.xd.xdial.debug.settings-ui"
    static let legacyProbeApplicationIdentifier =
        "com.kafeifei.xdial.ne-probe"
    static let legacyApplicationIdentifier = "com.kafeifei.xdial"
    static let legacyProbeSettingsUIIdentifier =
        "com.kafeifei.xdial.ne-probe.settings-ui"
    static let legacySettingsUIIdentifier =
        "com.kafeifei.xdial.settings-ui"
    static let legacyHelperIdentifier = "com.kafeifei.xdial.helper"
    static let legacyTransparentProxyIdentifier =
        "com.kafeifei.xdial.transparent-proxy"

    #if XDIAL_DEVELOPMENT_IDENTITY
    static let isDevelopment = true
    static let applicationIdentifier = developmentApplicationIdentifier
    static let helperIdentifier = "com.xd.xdial.debug.helper"
    static let daemonIdentifier = "com.xd.xdial.debug.daemon"
    static let transparentProxyIdentifier =
        "com.xd.xdial.debug.transparent-proxy"
    static let settingsUIIdentifier = developmentSettingsUIIdentifier
    static let appGroupIdentifier =
        "SX9RG894L5.com.xd.xdial.debug.network"
    static let applicationBundleName = "XDial Debug.app"
    /// Debug was installed under a misspelled name; installation migrates it.
    static let previousApplicationBundleNames = ["XDail Debug.app"]
    static let applicationDisplayName = "XDial Debug"
    static let productTitle = "XDial Debug"
    static let dataIdentifier = "com.kafeifei.xdial.debug"
    static let userDataDirectoryName = ".xdial-debug"
    static let applicationSupportDirectoryName = "XDial Debug"
    static let logDirectoryName = "XDial Debug"
    static let daemonSocketPath = "/tmp/xdial-debug.sock"
    static let engineRuntimePath = "/tmp/xdial-debug-engine"
    static let daemonLogPath = "/tmp/xdial-debug.log"
    static let registrationMaintenancePath =
        "/tmp/xdial-debug-registration-maintenance"
    static let debugServerPort: UInt16 = 19877
    static let allowsAutomaticUpdates = false
    static let allowsFormalDataMigration = false
    static let allowsLegacyCleanup = false
    #elseif XDIAL_STORE
    static let isDevelopment = false
    static let applicationIdentifier = "com.xd.xdial.ios"
    static let helperIdentifier = applicationIdentifier + ".helper"
    static let daemonIdentifier = applicationIdentifier + ".daemon"
    static let transparentProxyIdentifier =
        applicationIdentifier + ".tunnel"
    static let settingsUIIdentifier = applicationIdentifier + ".settings-ui"
    static let appGroupIdentifier =
        "group.com.xd.xdial.ios"
    static let applicationBundleName = "XDial.app"
    static let previousApplicationBundleNames: [String] = []
    static let applicationDisplayName = "XDial"
    static let productTitle = "XDial"
    static let dataIdentifier = applicationIdentifier
    static let userDataDirectoryName = formalUserDataDirectoryName
    static let applicationSupportDirectoryName = "XDial"
    static let logDirectoryName = "XDial"
    static let daemonSocketPath = "/tmp/xdial-store.sock"
    static let engineRuntimePath = "/tmp/xdial-store-engine"
    static let daemonLogPath = "/tmp/xdial-store.log"
    static let registrationMaintenancePath =
        "/tmp/xdial-store-registration-maintenance"
    static let debugServerPort: UInt16 = 19876
    static let allowsAutomaticUpdates = false
    static let allowsFormalDataMigration = false
    static let allowsLegacyCleanup = false

    #else
    static let isDevelopment = false
    static let applicationIdentifier = formalApplicationIdentifier
    static let helperIdentifier = formalHelperIdentifier
    static let daemonIdentifier = formalDaemonIdentifier
    static let transparentProxyIdentifier =
        formalTransparentProxyIdentifier
    static let settingsUIIdentifier = formalSettingsUIIdentifier
    static let appGroupIdentifier =
        "SX9RG894L5.com.xd.xdial.network"
    static let applicationBundleName = "XDial.app"
    static let previousApplicationBundleNames: [String] = []
    static let applicationDisplayName = "XDial"
    static let productTitle = "XDial"
    static let dataIdentifier = formalDataIdentifier
    static let userDataDirectoryName = formalUserDataDirectoryName
    static let applicationSupportDirectoryName = "XDial"
    static let logDirectoryName = "XDial"
    static let daemonSocketPath = "/tmp/xdial.sock"
    static let engineRuntimePath = "/tmp/xdial-engine"
    static let daemonLogPath = "/tmp/xdial.log"
    static let registrationMaintenancePath =
        "/tmp/xdial-registration-maintenance"
    static let debugServerPort: UInt16 = 19876
    static let allowsAutomaticUpdates = true
    static let allowsFormalDataMigration = true
    static let allowsLegacyCleanup = true
    #endif

    static let applicationDestinationURL = URL(
        fileURLWithPath: "/Applications/\(applicationBundleName)",
        isDirectory: true
    )
    static let previousApplicationDestinationURLs = previousApplicationBundleNames.map {
        URL(fileURLWithPath: "/Applications/\($0)", isDirectory: true)
    }
    // Every independently installed identity is a known sibling: Debug and
    // the formal app each exclude the other when checking whether their own
    // helper has exited during a registration refresh. A retired channel is
    // not listed, so its leftover process stays unknown and pauses maintenance.
    struct InstalledChannel {
        let applicationIdentifier: String
        let bundleName: String
        let daemonSocketPath: String
        var bundleURL: URL { URL(fileURLWithPath: "/Applications/\(bundleName)", isDirectory: true) }
    }
    static let installedChannels: [InstalledChannel] = [
        .init(applicationIdentifier: "com.xd.xdial.app", bundleName: "XDial.app", daemonSocketPath: "/tmp/xdial.sock"),
        .init(applicationIdentifier: "com.xd.xdial.debug", bundleName: "XDial Debug.app", daemonSocketPath: "/tmp/xdial-debug.sock"),
        // Debug stays recognizable at its previous path until it migrates.
        .init(applicationIdentifier: "com.xd.xdial.debug", bundleName: "XDail Debug.app", daemonSocketPath: "/tmp/xdial-debug.sock"),
    ]
    static let siblingChannels = installedChannels.filter { $0.applicationIdentifier != applicationIdentifier }
    static let siblingApplicationDestinationURLs = siblingChannels.map(\.bundleURL)
    static let settingsEntryTypeIdentifier =
        applicationIdentifier + ".settings-entry"
    static let settingsDockActivationNotification =
        dataIdentifier + ".settings-dock.activate"
    static let settingsDockDismissalNotification =
        dataIdentifier + ".settings-dock.dismiss"
    static let providerConfigurationName =
        applicationDisplayName + " Transparent Proxy"
    /// The extension's CFBundleDisplayName from project.yml, as System
    /// Settings and `systemextensionsctl` show it. No localization overrides it.
    static let transparentProxyDisplayName =
        applicationDisplayName + " Network Extension"
    static let queueLabelPrefix = applicationIdentifier
    static let updateStagingDirectoryName = isDevelopment
        ? "XDialDebugUpdates" : "XDialUpdates"
    static let installationArtifactPrefix = isDevelopment
        ? ".Xdial-debug" : ".XDial"
}
