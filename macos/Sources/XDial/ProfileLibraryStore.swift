import CryptoKit
import Darwin
import Foundation
import LocalAuthentication
import Security

/// The complete local working copy (including subscription URLs and baselines)
/// is authenticated and encrypted. Only a random AES key lives in Keychain.
/// A locked/missing key or corrupt file is an error, never an empty new library.
final class ProfileLibraryStore {
    let directory: URL
    private let keyProvider: ((Bool) throws -> SymmetricKey)?
    private var cachedKey: SymmetricKey?
    private let legacyKeychainService: String?
    private let keychainService: String
    private var loadedDigest: Data?
    private var needsSchemaBackup = false
    private var fileURL: URL { directory.appendingPathComponent("profiles.enc") }

    init(directory: URL = ConfigurationStorage.directory(),
         keychainService: String = ConfigurationStorage.keychainService,
         legacyKeychainService: String? = nil,
         keyProvider: ((Bool) throws -> SymmetricKey)? = nil) {
        self.directory = directory
        self.keychainService = keychainService
        self.legacyKeychainService = legacyKeychainService ?? (keychainService == ConfigurationStorage.keychainService
            ? ConfigurationStorage.legacyConfigurationKeychainService : nil)
        self.keyProvider = keyProvider
    }

    func load(readOnly: Bool = false) throws -> ProfileLibrary? {
        guard let data = try readEncryptedFile() else {
            if !readOnly { loadedDigest = nil }
            return nil
        }
        let box = try AES.GCM.SealedBox(combined: data)
        let (plaintext, verifiedKey, legacy) = try decryptLibrary(box, readOnly: readOnly)
        let decoded = try JSONDecoder().decode(ProfileLibrary.self, from: plaintext)
        let library = try decoded.validated()
        guard !readOnly else { return library }
        if legacy { try rememberLegacyKey(verifiedKey) }
        needsSchemaBackup = decoded.schemaVersion < 3
        cachedKey = verifiedKey
        loadedDigest = Data(SHA256.hash(data: data))
        return library
    }

    /// Only an absent library permits migration. An unreadable existing library
    /// is never replaced by a legacy snapshot or an empty default.
    func loadOrCreate(migrate: () throws -> ProfileLibrary?) throws -> ProfileLibrary {
        if let existing = try load() { return existing }
        let library = try migrate() ?? ProfileLibrary()
        // Another version may have finished first while conversion was running.
        if let existing = try load() { return existing }
        try save(library)
        return library
    }

    func save(_ library: ProfileLibrary) throws {
        let data = try JSONEncoder().encode(library.validated())
        guard data.count <= 32 * 1024 * 1024 else {
            throw ProfileLibraryError.invalid("配置库超过大小限制")
        }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let descriptor = open(directory.appendingPathComponent("profiles.lock").path,
                              O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, mode_t(0o600))
        guard descriptor >= 0 else { throw ProfileLibraryError.invalid("无法锁定共享配置库，配置尚未保存") }
        defer { close(descriptor) }
        guard flock(descriptor, LOCK_EX | LOCK_NB) == 0 else {
            throw ProfileLibraryError.invalid("其他版本正在保存共享配置，请稍后重试")
        }
        defer { flock(descriptor, LOCK_UN) }
        let current = try readEncryptedFile()
        let currentDigest = current.map { Data(SHA256.hash(data: $0)) }
        guard currentDigest == loadedDigest else {
            throw ProfileLibraryError.invalid("共享配置已被其他版本更新或删除，当前修改未保存；请重新打开应用后再编辑")
        }
        if needsSchemaBackup, let current {
            let backup = directory.appendingPathComponent("profiles-before-global-" + UUID().uuidString + ".enc")
            try current.write(to: backup, options: [.withoutOverwriting])
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: backup.path)
            needsSchemaBackup = false
        }
        let sealed = try AES.GCM.seal(data, using: writingKey(create: current == nil),
                                      authenticating: Data("XDial Profile Library v1".utf8))
        guard let combined = sealed.combined else {
            throw ProfileLibraryError.invalid("无法加密配置库")
        }
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
        try combined.write(to: fileURL, options: [.atomic])
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: fileURL.path)
        loadedDigest = Data(SHA256.hash(data: combined))
    }

    private func readEncryptedFile() throws -> Data? {
        let handle: FileHandle
        do { handle = try FileHandle(forReadingFrom: fileURL) }
        catch let error as CocoaError where error.code == .fileReadNoSuchFile || error.code == .fileNoSuchFile { return nil }
        defer { try? handle.close() }
        let limit = 32 * 1024 * 1024 + 28
        let data = try handle.read(upToCount: limit + 1) ?? Data()
        guard data.count <= limit else { throw ProfileLibraryError.invalid("配置库超过大小限制") }
        return data
    }

    private static let account = "profile-library-key-v1"
    // The standalone and sandboxed apps can have separate encrypted files
    // while seeing the same login keychain. Preserve both valid keys when
    // one edition created a fresh library before the other edition migrated.
    private static let migratedAccount = "profile-library-migrated-key-v1"

    private func decryptLibrary(_ box: AES.GCM.SealedBox, readOnly: Bool) throws -> (Data, SymmetricKey, Bool) {
        func decrypt(_ key: SymmetricKey) throws -> Data {
            try AES.GCM.open(box, using: key, authenticating: Data("XDial Profile Library v1".utf8))
        }
        if let keyProvider {
            let key = try keyProvider(false)
            return (try decrypt(key), key, false)
        }
        if let cachedKey, let data = try? decrypt(cachedKey) { return (data, cachedKey, false) }
        var candidates = [(keychainService, Self.account, false)]
        if let legacyKeychainService {
            candidates.append((keychainService, Self.migratedAccount, false))
            candidates.append((legacyKeychainService, Self.account, true))
        }
        var found = false
        for (service, account, legacy) in candidates {
            guard let key = try readKey(service: service, account: account, readOnly: readOnly) else { continue }
            found = true
            if let data = try? decrypt(key) { return (data, key, legacy) }
        }
        if found { throw CryptoKitError.authenticationFailure }
        throw ProfileLibraryError.keychain(errSecItemNotFound)
    }

    private func rememberLegacyKey(_ key: SymmetricKey) throws {
        for account in [Self.account, Self.migratedAccount] {
            if try installIfAbsent(key, account: account) == key { return }
        }
        throw ProfileLibraryError.invalid("已有配置密钥与旧配置不匹配，原配置已保留")
    }

    private func writingKey(create: Bool) throws -> SymmetricKey {
        if let cachedKey { return cachedKey }
        let key: SymmetricKey
        if let keyProvider {
            key = try keyProvider(create)
        } else if let existing = try readKey(service: keychainService, account: Self.account) {
            key = existing
        } else {
            guard create else { throw ProfileLibraryError.keychain(errSecItemNotFound) }
            key = try installIfAbsent(SymmetricKey(size: .bits256), account: Self.account)
        }
        cachedKey = key
        return key
    }

    private func readKey(service: String, account: String, readOnly: Bool = false) throws -> SymmetricKey? {
        var output: CFTypeRef?
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: account,
            kSecReturnData as String: true, kSecMatchLimit as String: kSecMatchLimitOne
        ]
        if readOnly {
            let context = LAContext()
            context.interactionNotAllowed = true
            query[kSecUseAuthenticationContext as String] = context
        }
        let status = SecItemCopyMatching(query as CFDictionary, &output)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else { throw ProfileLibraryError.keychain(status) }
        guard let data = output as? Data, data.count == 32 else { throw ProfileLibraryError.keychain(errSecDecode) }
        return SymmetricKey(data: data)
    }

    // Never overwrite another edition's key. On a concurrent add, read the winner.
    private func installIfAbsent(_ key: SymmetricKey, account: String) throws -> SymmetricKey {
        let status = SecItemAdd([
            kSecClass: kSecClassGenericPassword, kSecAttrService: keychainService, kSecAttrAccount: account,
            kSecValueData: key.withUnsafeBytes { Data($0) },
            kSecAttrAccessible: kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        ] as CFDictionary, nil)
        if status == errSecSuccess { return key }
        if status == errSecDuplicateItem, let existing = try readKey(service: keychainService, account: account) {
            return existing
        }
        throw ProfileLibraryError.keychain(status)
    }
}
