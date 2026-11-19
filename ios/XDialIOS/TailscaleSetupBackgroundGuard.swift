import UIKit

/// Releases the isolated Tailscale setup's state lock before iOS suspends the app.
///
/// The setup runtime runs Libbox inside the app and holds a file lock in the App
/// Group container until it stops; RunningBoard terminates an app that is
/// suspended while holding such a lock (0xdead10cc). Leaving the app briefly
/// (for example to read a two-step code) must not invalidate a sign-in in
/// progress, so the setup keeps running for a short grace period under a
/// background task and is paused only if the app stays in the background, or
/// at once when the system ends the background time. Returning in time cancels
/// the pause; a paused setup resumes when the app is active again.
@MainActor
final class TailscaleSetupBackgroundGuard {
    struct BackgroundTasks {
        var begin: (_ expiration: @escaping @MainActor () -> Void) -> UIBackgroundTaskIdentifier
        var end: (UIBackgroundTaskIdentifier) -> Void

        static var application: BackgroundTasks {
            BackgroundTasks(
                begin: { expiration in
                    UIApplication.shared.beginBackgroundTask(withName: "Release Tailscale setup lock") {
                        MainActor.assumeIsolated { expiration() }
                    }
                },
                end: { UIApplication.shared.endBackgroundTask($0) }
            )
        }
    }

    static let defaultGrace: Duration = .seconds(20)

    private let app: AppState
    private let grace: Duration
    private let tasks: BackgroundTasks
    private var task: UIBackgroundTaskIdentifier = .invalid
    private var pendingPause: Task<Void, Never>?
    private var pausing = false
    private var isActive = false

    init(app: AppState, grace: Duration, tasks: BackgroundTasks) {
        self.app = app
        self.grace = grace
        self.tasks = tasks
    }

    convenience init(app: AppState) {
        self.init(app: app, grace: Self.defaultGrace, tasks: .application)
    }

    func enteredBackground() {
        isActive = false
        guard app.tailscaleSetupLineID != nil, pendingPause == nil, !pausing else { return }
        task = tasks.begin { [weak self] in self?.pause() }
        guard task != .invalid else {
            // No execution time was granted, so there is no protected grace period.
            pause()
            return
        }
        pendingPause = Task { [weak self, grace] in
            try? await Task.sleep(for: grace)
            guard !Task.isCancelled else { return }
            self?.pause()
        }
    }

    func becameActive() {
        isActive = true
        pendingPause?.cancel()
        pendingPause = nil
        guard !pausing else { return }
        endTask()
        app.resumeTailscaleSetupAfterBackground()
    }

    func becameInactive() {
        isActive = false
    }

    private func pause() {
        pendingPause?.cancel()
        pendingPause = nil
        guard !pausing, !isActive else { return }
        pausing = true
        app.pauseTailscaleSetupForBackground { [weak self] in
            guard let self else { return }
            self.pausing = false
            self.endTask()
            // A foreground transition during stop does not promise that the
            // app is still active now. Keep the paused line until it really is.
            if self.isActive { self.app.resumeTailscaleSetupAfterBackground() }
        }
    }

    private func endTask() {
        guard task != .invalid else { return }
        tasks.end(task)
        task = .invalid
    }
}
