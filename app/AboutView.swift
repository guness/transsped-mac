import SwiftUI

struct AboutView: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 14) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 72, height: 72)
            VStack(spacing: 2) {
                Text("TransSped").font(.title2).bold()
                Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
            }
            Text("Log in to ANAF SPV from macOS Firefox using your Trans Sped cloud qualified certificate.")
                .multilineTextAlignment(.center).font(.callout).foregroundStyle(.secondary)
            Callout(icon: "lock.shield",
                    text: "Signing is delegated to the Trans Sped cloud — no private key is ever stored on this Mac.",
                    tint: Theme.brand)
            Link(destination: URL(string: "https://github.com/guness/transsped-mac")!) {
                Label("github.com/guness/transsped-mac", systemImage: "arrow.up.forward")
            }
            .font(.callout)
            Button("Close") { dismiss() }.keyboardShortcut(.defaultAction).controlSize(.large)
        }
        .padding(24)
        .frame(width: 340)
    }
}
