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
        VStack(alignment: .leading, spacing: 16) {
            header
            Group {
                if loading {
                    HStack { Spacer(); ProgressView(); Spacer() }.padding(.vertical, 28)
                } else if status == nil {
                    engineError
                } else if let s = status, s.installed {
                    installed(s)
                } else {
                    setupCard
                }
            }
            if let m = message {
                Text(m)
                    .font(.callout)
                    .foregroundStyle(isError ? Color.red : Color.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(20)
        .frame(width: 380)
        .task { await refresh() }
        .sheet(isPresented: $showAbout) { AboutView() }
    }

    // MARK: - Header

    private var header: some View {
        HStack(spacing: 12) {
            Image(nsImage: NSApplication.shared.applicationIconImage)
                .resizable().frame(width: 44, height: 44)
            VStack(alignment: .leading, spacing: 1) {
                Text("TransSped").font(.title3).fontWeight(.semibold)
                Text("v\(appVersion())").font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Button { showAbout = true } label: {
                Image(systemName: "info.circle").font(.system(size: 16))
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
            .help("About TransSped")
        }
    }

    // MARK: - States

    private var engineError: some View {
        Card {
            VStack(spacing: 10) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.largeTitle).foregroundStyle(.orange)
                Text("Couldn't run the setup engine.").font(.headline)
                Text("The app may be damaged — reinstall TransSped from the DMG.")
                    .font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
                Button("Retry") { Task { await refresh() } }.buttonStyle(.bordered)
            }
            .frame(maxWidth: .infinity)
        }
    }

    private var setupCard: some View {
        VStack(spacing: 14) {
            Card {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Set up TransSped").font(.headline)
                    Text("Enter the email or phone registered with Trans Sped for your cloud certificate.")
                        .font(.callout).foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    TextField("email or phone", text: $userID)
                        .textFieldStyle(.roundedBorder)
                        .controlSize(.large)
                        .onSubmit { Task { await doSetup(user: userID) } }
                }
            }
            Button { Task { await doSetup(user: userID) } } label: {
                Label("Set up", systemImage: "checkmark.seal").frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent).controlSize(.large).tint(Theme.brand)
            .disabled(busy || userID.trimmingCharacters(in: .whitespaces).isEmpty)
            if busy { ProgressView().controlSize(.small) }
        }
    }

    private func installed(_ s: EngineStatus) -> some View {
        VStack(spacing: 14) {
            Card {
                VStack(spacing: 10) {
                    HStack(spacing: 10) {
                        Image(systemName: s.moduleRegistered ? "checkmark.seal.fill" : "exclamationmark.triangle.fill")
                            .foregroundStyle(s.moduleRegistered ? Color.green : Color.orange)
                        Text(s.moduleRegistered ? "Installed in Firefox" : "Not registered in Firefox")
                            .fontWeight(.semibold)
                        Spacer()
                    }
                    Divider()
                    InfoRow(icon: "person.crop.circle", label: "Account", value: s.account)
                    if !s.certNotAfter.isEmpty {
                        certRow(s.certNotAfter)
                    }
                }
            }
            if s.firefoxRunning {
                Callout(icon: "exclamationmark.triangle.fill",
                        text: "Firefox is open — quit it before Update or Uninstall.",
                        tint: .orange)
            }
            VStack(spacing: 8) {
                Button { openANAF() } label: {
                    Label("Open ANAF login", systemImage: "arrow.up.forward.app").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent).controlSize(.large).tint(Theme.brand)
                Button { Task { await doSetup(user: s.account) } } label: {
                    Label("Update certificate", systemImage: "arrow.clockwise").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered).controlSize(.large).disabled(busy)
            }
            HStack {
                if busy { ProgressView().controlSize(.small) }
                Spacer()
                Button(role: .destructive) { Task { await doUninstall() } } label: {
                    Text("Uninstall").font(.callout)
                }
                .buttonStyle(.plain).foregroundStyle(.red).disabled(busy)
            }
        }
    }

    // A cert-expiry row whose value colours by remaining validity.
    private func certRow(_ iso: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "checkmark.shield").foregroundStyle(.secondary).frame(width: 16)
            Text("Valid until").foregroundStyle(.secondary)
            Spacer(minLength: 12)
            Text(formatDate(iso)).fontWeight(.medium).monospacedDigit()
                .foregroundStyle(expiryColor(iso))
        }
        .font(.callout)
    }

    // MARK: - Actions

    private func refresh() async {
        loading = true
        status = await Engine.status()
        loading = false
    }

    private func doSetup(user: String) async {
        busy = true; message = nil
        let r = await Engine.setup(user: user.trimmingCharacters(in: .whitespaces))
        if r.ok {
            if let s = r.status { status = s } else { status = await Engine.status() }
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
        let url = URL(string: "https://www.anaf.ro/")!
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

    private func parseDate(_ iso: String) -> Date? { ISO8601DateFormatter().date(from: iso) }

    private func formatDate(_ iso: String) -> String {
        guard let d = parseDate(iso) else { return iso }
        let f = DateFormatter(); f.dateStyle = .medium; f.timeStyle = .none
        return f.string(from: d)
    }

    private func expiryColor(_ iso: String) -> Color {
        guard let d = parseDate(iso) else { return .primary }
        let days = d.timeIntervalSinceNow / 86400
        if days < 0 { return .red }
        if days < 30 { return .orange }
        return .primary
    }
}
