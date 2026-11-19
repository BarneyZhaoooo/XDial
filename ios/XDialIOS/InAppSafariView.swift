import SafariServices
import SwiftUI

/// A page opened inside XDial, so the app stays in the foreground while the user
/// signs in (an isolated Tailscale setup must keep running to receive the result).
struct InAppSafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        SFSafariViewController(url: url)
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

/// A Tailscale sign-in page shown with `InAppSafariView`.
struct TailscaleLoginPage: Identifiable, Equatable {
    let url: URL
    var id: URL { url }
}
