#if DEBUG
import Foundation

/// Packaging acceptance for the company preset, run by a built Debug app:
///
///     XDial --check-seed-acceptance <older package SeedProfile directory> <output directory>
///
/// It runs the launch-time seed decision (`ProfileLibraryMigration.applying`) with
/// this package's embedded preset and the real importer, entirely in memory: no
/// Keychain, configuration library, ledger or network is touched. Each case writes
/// the effective configuration (`<case>.json`) for `xdial-seed effective` to compare
/// with the phone preset by routing meaning. Credentials are fixed test values.
enum SeedAcceptance {
    private static let profileID = "seed" // the namespace xdial-seed imports the authority with

    private struct Report: Encodable {
        let profile: Profile
        let regionTargets: [String: String]
        let activeScenarioID: String
        let notes: [String]
    }

    static func run(olderSeedDirectory: String, output: String) -> Never {
        do {
            let directory = URL(fileURLWithPath: output, isDirectory: true)
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            guard let seed = try SeedProfile.load() else { throw ProfileLibraryError.invalid("this package embeds no preset") }
            guard let older = try SeedProfile.load(directory: URL(fileURLWithPath: olderSeedDirectory, isDirectory: true)) else {
                throw ProfileLibraryError.invalid("the older package embeds no preset")
            }
            let importDocument: (String, String) throws -> Profile = {
                try ProfileDocumentService.importDocument($0, profileID: $1)
            }
            func launch(_ seed: SeedProfile, createdProfileID: String?, entry: SeedProfileLedger.Entry?,
                        library: ProfileLibrary, targets: [String: String]) throws -> ProfileLibraryMigration.SeedApplication {
                let result = ProfileLibraryMigration.applying(seed, createdProfileID: createdProfileID, entry: entry,
                                                              to: library, currentRegionTargets: targets,
                                                              importDocument: importDocument)
                if let failure = result.failure { throw failure }
                return result
            }
            func write(_ name: String, _ library: ProfileLibrary, _ targets: [String: String], _ notes: [String]) throws {
                let encoder = JSONEncoder()
                encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
                let report = Report(profile: library.snapshot(), regionTargets: targets,
                                    activeScenarioID: library.activeScenarioID, notes: notes)
                try encoder.encode(report).write(to: directory.appendingPathComponent(name + ".json"))
            }

            // 1. Fresh install of this package.
            let fresh = try ProfileLibraryMigration.seedLibrary(seed, id: profileID, importDocument: importDocument).required()
            let first = try launch(seed, createdProfileID: profileID, entry: nil, library: fresh, targets: [:])
            let again = try launch(seed, createdProfileID: nil, entry: first.entry, library: fresh,
                                   targets: first.regionTargets ?? [:])
            try write("fresh", fresh, first.regionTargets ?? [:],
                      again.library == nil && again.entry == nil ? ["second launch changed nothing"] : ["second launch changed the library"])

            // 2. The older package installed first; the employee signed in and picked a Scenario by hand.
            var old = try ProfileLibraryMigration.seedLibrary(older, id: profileID, importDocument: importDocument).required()
            let oldLaunch = try launch(older, createdProfileID: profileID, entry: nil, library: old, targets: [:])
            let record = try old.profiles.firstIndex { $0.id == profileID }.required()
            for index in old.profiles[record].profile.lines.indices where old.profiles[record].profile.lines[index].type == "vpn" {
                old.profiles[record].profile.lines[index].vpnUsername = "acceptance-user"
                old.profiles[record].profile.lines[index].vpnPassword = "acceptance-password"
            }
            let manual = try (old.scenarios.first { $0.id != old.activeScenarioID } ?? old.scenarios.first).required().id
            old.activeScenarioID = manual
            old = try old.validated()
            let upgrade = try launch(seed, createdProfileID: nil, entry: oldLaunch.entry, library: old,
                                     targets: oldLaunch.regionTargets ?? [:])
            let upgraded = try upgrade.library.required()
            let repeated = try launch(seed, createdProfileID: nil, entry: upgrade.entry, library: upgraded,
                                      targets: upgrade.regionTargets ?? [:])
            let line = upgraded.snapshot().lines.first { $0.type == "vpn" }
            try write("upgrade", upgraded, upgrade.regionTargets ?? [:], [
                "manual Scenario before upgrade: \(manual)",
                line?.vpnUsername == "acceptance-user" && line?.vpnPassword == "acceptance-password"
                    ? "credentials kept" : "credentials lost",
                repeated.library == nil && repeated.entry == nil ? "second launch changed nothing" : "second launch changed the library",
            ])

            // 3. The older package joined beside the employee's own Profile; they changed one of its Scenarios.
            let own = try ProfileLibraryMigration.starterLibrary()
            let joined = try launch(older, createdProfileID: nil, entry: nil, library: own, targets: [:])
            var beside = try joined.library.required()
            let edited = try beside.scenarios.firstIndex { beside.scenarioSources[$0.id] == joined.entry?.profileID && !$0.bindings.isEmpty }.required()
            beside.scenarios[edited].name = "我的国内"
            beside.scenarios[edited].bindings.removeAll()
            beside.scenarios[edited].matchOrder.removeAll()
            beside = try beside.validated()
            let ownActive = beside.activeScenarioID
            let besideUpgrade = try launch(seed, createdProfileID: nil, entry: joined.entry, library: beside,
                                           targets: joined.regionTargets ?? [:])
            let besideUpgraded = try besideUpgrade.library.required()
            let besideID = try joined.entry.required().profileID
            var notes = ProfileLibraryMigration.keptCustomizations(besideUpgraded.scenarios,
                                                                   seed: try importDocument(seed.text, besideID),
                                                                   prefix: seed.name + " · ")
            notes.append(besideUpgraded.activeScenarioID == ownActive ? "own active Scenario kept" : "own active Scenario changed")
            notes.append("profile id: \(besideID)")
            try write("beside", besideUpgraded, besideUpgrade.regionTargets ?? [:], notes)

            // 4. The employee deleted the older preset's Profile: the upgrade must not add it back.
            let deleted = try beside.removingProfile(besideID)
            let afterDelete = try launch(seed, createdProfileID: nil, entry: joined.entry, library: deleted,
                                         targets: joined.regionTargets ?? [:])
            try write("deleted", deleted, afterDelete.regionTargets ?? [:],
                      [afterDelete.library == nil && afterDelete.entry == nil ? "deleted preset stayed deleted" : "deleted preset came back"])
            fputs("seed acceptance written to \(directory.path)\n", stdout)
            exit(0)
        } catch {
            fputs("seed acceptance failed: \(error)\n", stderr)
            exit(1)
        }
    }
}

private extension Optional {
    func required() throws -> Wrapped {
        guard let self else { throw ProfileLibraryError.invalid("seed acceptance: unexpected empty value") }
        return self
    }
}
#endif
