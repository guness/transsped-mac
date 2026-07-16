import SwiftUI

struct AboutView: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 12) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 72, height: 72)
            Text("TransSped").font(.title2).bold()
            Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
            Text("Log in to ANAF SPV from macOS Firefox using your Trans Sped cloud qualified certificate.")
                .multilineTextAlignment(.center).font(.callout)
            Text("Signing is delegated to the Trans Sped cloud — no private key is ever stored on this Mac.")
                .multilineTextAlignment(.center).font(.caption).foregroundStyle(.secondary)
            Link("github.com/guness/transsped-mac",
                 destination: URL(string: "https://github.com/guness/transsped-mac")!)
                .font(.callout)
            Button("Close") { dismiss() }.keyboardShortcut(.defaultAction)
        }
        .padding(24)
        .frame(width: 340)
    }
}
