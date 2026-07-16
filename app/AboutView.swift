import SwiftUI

struct AboutView: View {
    @AppStorage("appLanguage") private var langRaw = AppLang.system.rawValue
    @Environment(\.dismiss) private var dismiss
    private var L: Lang { resolve(langRaw) }

    var body: some View {
        VStack(spacing: 14) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 72, height: 72)
            VStack(spacing: 2) {
                Text("TransSped").font(.title2).bold()
                Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
            }
            Text(t(.aboutDesc, L))
                .multilineTextAlignment(.center).font(.callout).foregroundStyle(.secondary)
            Callout(icon: "lock.shield", text: t(.aboutSecurity, L), tint: Theme.brand)
            Link(destination: URL(string: "https://github.com/guness/transsped-mac")!) {
                Label("github.com/guness/transsped-mac", systemImage: "arrow.up.forward")
            }
            .font(.callout)
            Button(t(.close, L)) { dismiss() }.keyboardShortcut(.defaultAction).controlSize(.large)
        }
        .padding(24)
        .frame(width: 340)
    }
}
