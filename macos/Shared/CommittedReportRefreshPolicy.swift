import Foundation

/// Decides whether the Host shows a newer copy of a committed report.
///
/// The Provider keeps writing to its report after the commit: every local Line
/// recovery moves a task and appends events. The Host used to read that report
/// only while a transaction was being built and when the system session
/// changed state, so a session that simply stayed connected showed the report
/// as it stood at the last such moment.
///
/// The periodic refresh is for display only. It adopts nothing but a still
/// committed report of the session the Host already proved connected, and it
/// stands aside whenever the Host is itself changing the connection: start,
/// recovery and Scenario switch each read the report on their own terms.
enum CommittedReportRefreshPolicy {
    static let interval: TimeInterval = 5

    static func shouldAdopt(
        probed: ConnectionReport,
        presented: ConnectionReport?,
        currentTransactionID: String?,
        connectedSessionTransactionID: String?,
        systemSessionConnected: Bool,
        hostIsChangingConnection: Bool
    ) -> Bool {
        guard
            systemSessionConnected,
            !hostIsChangingConnection,
            !probed.transactionID.isEmpty,
            probed.transactionID == currentTransactionID,
            probed.transactionID == connectedSessionTransactionID,
            probed.state == .committed,
            probed.error == nil,
            probed.rollbackError == nil,
            !probed.systemTakeoverRemoved
        else {
            return false
        }
        guard let presented else { return true }
        guard presented.transactionID == probed.transactionID else {
            return false
        }
        // The Host appends its own events to its copy in a few places. A
        // Provider copy that has not caught up with them must not erase them.
        guard
            (probed.events.last?.sequence ?? 0)
                >= (presented.events.last?.sequence ?? 0)
        else {
            return false
        }
        return probed != presented
    }
}
