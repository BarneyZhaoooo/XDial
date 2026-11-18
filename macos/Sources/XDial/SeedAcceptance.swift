#if DEBUG
import Foundation

/// Packaging acceptance for the company preset, run by a built Debug app:
///
///     XDial --check-seed-acceptance <older package SeedProfile directory> <output directory>
///
/// It runs the launch-time seed decision (`ProfileLibraryMigration.applying`) with
/// this package's embedded preset and the real importer, entirely in memory: no
/// Keychain, configuration library, ledger or network is touched. Each case writes
/// the effective configuration (`<case>.json`) together with the package's own preset as
/// imported for that Profile; `xdial-seed effective` compares the two by routing meaning,
/// and `xdial-seed check` separately proves the package preset equals the phone's.
/// Credentials are fixed test values. Conditions every upgrade must meet (credentials,
/// manual selection, no change on a second launch, a deleted preset staying deleted)
/// fail the run; customizations the user is allowed to keep are written as notes.
enum SeedAcceptance {
    private static let profileID = "seed" // the namespace xdial-seed imports the authority with

    private struct Report: Encodable {
        let profile: Profile
        let regionTargets: [String: String]
        let activeScenarioID: String
        let notes: [String]
        /// The package's preset imported for the same Profile: what an untouched configuration must equal.
        let expected: Profile
        let expectedRegionTargets: [String: String]
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
            /// A launch that found nothing to do returns no library or entry.
            func unchanged(_ result: ProfileLibraryMigration.SeedApplication) -> Bool {
                result.library == nil && result.entry == nil
            }
            func write(_ name: String, _ library: ProfileLibrary, _ targets: [String: String], _ notes: [String],
                       profileID: String = profileID) throws {
                let encoder = JSONEncoder()
                encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
                let expected = try importDocument(seed.text, profileID)
                let report = Report(profile: library.snapshot(), regionTargets: targets,
                                    activeScenarioID: library.activeScenarioID, notes: notes,
                                    expected: expected, expectedRegionTargets: seed.regionTargets(in: expected))
                try encoder.encode(report).write(to: directory.appendingPathComponent(name + ".json"))
            }

            // 1. Fresh install of this package.
            let fresh = try ProfileLibraryMigration.seedLibrary(seed, id: profileID, importDocument: importDocument).required()
            let first = try launch(seed, createdProfileID: profileID, entry: nil, library: fresh, targets: [:])
            let again = try launch(seed, createdProfileID: nil, entry: first.entry, library: fresh,
                                   targets: first.regionTargets ?? [:])
            try require(unchanged(again), "fresh install: the second launch changed the library")
            try write("fresh", fresh, first.regionTargets ?? [:], ["second launch changed nothing"])

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
            // An app update that keeps the same preset revision has nothing to apply.
            let upgraded = upgrade.library ?? old
            let upgradeEntry = upgrade.entry ?? oldLaunch.entry
            let upgradeTargets = upgrade.regionTargets ?? oldLaunch.regionTargets ?? [:]
            let repeated = try launch(seed, createdProfileID: nil, entry: upgradeEntry, library: upgraded,
                                      targets: upgradeTargets)
            let line = upgraded.snapshot().lines.first { $0.type == "vpn" }
            try require(line?.vpnUsername == "acceptance-user" && line?.vpnPassword == "acceptance-password",
                        "upgrade: the signed-in credentials were lost")
            try require(upgraded.activeScenarioID == manual, "upgrade: the manually selected Scenario changed")
            try require(unchanged(repeated), "upgrade: the second launch changed the library")
            try write("upgrade", upgraded, upgradeTargets, [
                "manual Scenario kept: \(manual)", "credentials kept", "second launch changed nothing",
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
            let besideUpgraded = besideUpgrade.library ?? beside
            let besideID = try joined.entry.required().profileID
            var notes = ProfileLibraryMigration.keptCustomizations(besideUpgraded.scenarios,
                                                                   seed: try importDocument(seed.text, besideID),
                                                                   prefix: seed.name + " · ")
            try require(besideUpgraded.activeScenarioID == ownActive, "beside: the user's own active Scenario changed")
            try require(besideUpgraded.scenarios.contains { $0.id == beside.scenarios[edited].id && $0.name == "我的国内" },
                        "beside: the Scenario the user changed was overwritten")
            notes.append("own active Scenario kept")
            notes.append("profile id: \(besideID)")
            try write("beside", besideUpgraded, besideUpgrade.regionTargets ?? joined.regionTargets ?? [:], notes,
                      profileID: besideID)

            // 4. The employee deleted the older preset's Profile: the upgrade must not add it back.
            let deleted = try beside.removingProfile(besideID)
            let afterDelete = try launch(seed, createdProfileID: nil, entry: joined.entry, library: deleted,
                                         targets: joined.regionTargets ?? [:])
            try require(unchanged(afterDelete), "deleted: the deleted preset came back")
            try write("deleted", deleted, afterDelete.regionTargets ?? [:], ["deleted preset stayed deleted"],
                      profileID: besideID)
            fputs("seed acceptance written to \(directory.path)\n", stdout)
            exit(0)
        } catch {
            fputs("seed acceptance failed: \(error)\n", stderr)
            exit(1)
        }
    }
}

/// A condition the acceptance must meet; failing it fails the whole run.
private func require(_ condition: Bool, _ failure: String) throws {
    guard condition else { throw ProfileLibraryError.invalid("seed acceptance: \(failure)") }
}

private extension Optional {
    func required() throws -> Wrapped {
        guard let self else { throw ProfileLibraryError.invalid("seed acceptance: unexpected empty value") }
        return self
    }
}
#endif
