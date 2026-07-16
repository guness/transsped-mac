import SwiftUI

struct ContentView: View {
    @State private var status: EngineStatus?
    @State private var loading = true
    @State private var busy = false
    @State private var message: String?
    @State private var isError = false
    @State private var userID = ""
    @State private var showAbout = false

    var body: some View {
        VStack(spacing: 16) {
            header
            if loading {
                ProgressView().padding()
            } else if status == nil {
                engineError
            } else if let s = status, s.installed {
                installed(s)
            } else {
                setupCard
            }
            if let m = message {
                Text(m).font(.callout)
                    .foregroundStyle(isError ? Color.red : Color.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(24)
        .frame(width: 380)
        .task { await refresh() }
        .sheet(isPresented: $showAbout) { AboutView() }
    }

    private var header: some View {
        VStack(spacing: 6) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 64, height: 64)
            Text("TransSped").font(.title2).bold()
            Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
        }
    }

    private var engineError: some View {
        VStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.orange).font(.largeTitle)
            Text("Couldn't run the setup engine.").font(.headline)
            Text("The app may be damaged — reinstall TransSped from the DMG.")
                .font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
            Button("Retry") { Task { await refresh() } }
        }
    }

    private var setupCard: some View {
        VStack(spacing: 12) {
            Text("Set up TransSped").font(.headline)
            Text("Enter the email or phone registered with Trans Sped for your cloud certificate.")
                .font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.center)
            TextField("email or phone", text: $userID).textFieldStyle(.roundedBorder)
            Button("Set up") { Task { await doSetup(user: userID) } }
                .buttonStyle(.borderedProminent)
                .disabled(busy || userID.trimmingCharacters(in: .whitespaces).isEmpty)
            if busy { ProgressView() }
        }
    }

    private func installed(_ s: EngineStatus) -> some View {
        VStack(spacing: 14) {
            statusRows(s)
            HStack {
                Button("Update") { Task { await doSetup(user: s.account) } }.disabled(busy)
                Button("Open ANAF login") { openANAF() }
            }
            HStack {
                Button("Uninstall", role: .destructive) { Task { await doUninstall() } }.disabled(busy)
                Button("About") { showAbout = true }
            }
            if busy { ProgressView() }
        }
    }

    private func statusRows(_ s: EngineStatus) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            row(s.moduleRegistered ? "checkmark.circle.fill" : "exclamationmark.triangle.fill",
                s.moduleRegistered ? "Installed in Firefox" : "Not registered in Firefox",
                s.moduleRegistered ? .green : .orange)
            row("person.crop.circle", "Account: \(s.account)", .secondary)
            if !s.certNotAfter.isEmpty {
                row("calendar", "Certificate valid until \(formatDate(s.certNotAfter))", expiryColor(s.certNotAfter))
            }
            if s.firefoxRunning {
                row("exclamationmark.triangle.fill", "Firefox is open — quit it before Update / Uninstall", .orange)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func row(_ icon: String, _ text: String, _ color: Color) -> some View {
        HStack(spacing: 8) {
            Image(systemName: icon).foregroundStyle(color)
            Text(text).font(.callout)
            Spacer()
        }
    }

    // MARK: - actions

    private func refresh() async {
        loading = true
        status = await Engine.status()
        loading = false
    }

    private func doSetup(user: String) async {
        busy = true; message = nil
        let r = await Engine.setup(user: user.trimmingCharacters(in: .whitespaces))
        if r.ok {
            if let s = r.status {
                status = s
            } else {
                status = await Engine.status()
            }
            message = r.message; isError = false
        } else {
            message = friendly(r); isError = true
        }
        busy = false
    }

    private func doUninstall() async {
        busy = true; message = nil
        let r = await Engine.uninstall()
        if r.ok {
            status = await Engine.status()
            message = ((r.notes ?? []) + [r.message ?? "Uninstalled."]).joined(separator: "\n")
            isError = false
        } else {
            message = friendly(r); isError = true
        }
        busy = false
    }

    private func openANAF() {
        let url = URL(string: "https://pfinternet.anaf.ro")!
        let firefox = URL(fileURLWithPath: "/Applications/Firefox.app")
        NSWorkspace.shared.open([url], withApplicationAt: firefox,
                                configuration: NSWorkspace.OpenConfiguration()) { _, _ in }
    }

    private func friendly(_ r: EngineResult) -> String {
        switch r.code {
        case "firefox_running": return "Please quit Firefox first, then try again."
        case "no_credential":  return "No certificate was found for this userID. Check it — or you may still need to enroll with Trans Sped (ANAF form 150)."
        case "no_profile":     return "No Firefox profile found. Launch Firefox once, then quit it and try again."
        case "network":        return "Couldn't reach the Trans Sped service. Check your connection and try again."
        default:               return r.error ?? "Something went wrong."
        }
    }

    private func parseDate(_ iso: String) -> Date? {
        ISO8601DateFormatter().date(from: iso)
    }

    private func formatDate(_ iso: String) -> String {
        guard let d = parseDate(iso) else { return iso }
        let f = DateFormatter(); f.dateStyle = .medium; f.timeStyle = .none
        return f.string(from: d)
    }

    private func expiryColor(_ iso: String) -> Color {
        guard let d = parseDate(iso) else { return .secondary }
        let days = d.timeIntervalSinceNow / 86400
        if days < 0 { return .red }
        if days < 30 { return .orange }
        return .secondary
    }
}
