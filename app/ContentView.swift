import SwiftUI

struct ContentView: View {
    @AppStorage("appLanguage") private var langRaw = AppLang.system.rawValue
    @State private var status: EngineStatus?
    @State private var loading = true
    @State private var busy = false
    @State private var message: String?
    @State private var isError = false
    @State private var userID = ""
    @State private var showAbout = false

    // Resolved language for this render; changing langRaw re-renders (live switch).
    private var L: Lang { resolve(langRaw) }

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
            Menu {
                Picker(t(.language, L), selection: $langRaw) {
                    Text(t(.langSystem, L)).tag(AppLang.system.rawValue)
                    Text("English").tag(AppLang.en.rawValue)
                    Text("Română").tag(AppLang.ro.rawValue)
                }
            } label: {
                Image(systemName: "globe").font(.system(size: 15))
            }
            .menuStyle(.borderlessButton).fixedSize()
            .foregroundStyle(.secondary).help(t(.language, L))
            Button { showAbout = true } label: {
                Image(systemName: "info.circle").font(.system(size: 16))
            }
            .buttonStyle(.plain).foregroundStyle(.secondary).help(t(.aboutTip, L))
        }
    }

    // MARK: - States

    private var engineError: some View {
        Card {
            VStack(spacing: 10) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.largeTitle).foregroundStyle(.orange)
                Text(t(.engineErrTitle, L)).font(.headline)
                Text(t(.engineErrBody, L))
                    .font(.callout).foregroundStyle(.secondary).multilineTextAlignment(.center)
                Button(t(.retry, L)) { Task { await refresh() } }.buttonStyle(.bordered)
            }
            .frame(maxWidth: .infinity)
        }
    }

    private var setupCard: some View {
        VStack(spacing: 14) {
            Card {
                VStack(alignment: .leading, spacing: 8) {
                    Text(t(.setUpTitle, L)).font(.headline)
                    Text(t(.setUpBlurb, L))
                        .font(.callout).foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                    TextField(t(.emailOrPhone, L), text: $userID)
                        .textFieldStyle(.roundedBorder)
                        .controlSize(.large)
                        .onSubmit { Task { await doSetup(user: userID) } }
                }
            }
            Button { Task { await doSetup(user: userID) } } label: {
                Label(t(.setUp, L), systemImage: "checkmark.seal").frame(maxWidth: .infinity)
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
                        Text(s.moduleRegistered ? t(.installed, L) : t(.notInstalled, L))
                            .fontWeight(.semibold)
                        Spacer()
                    }
                    Divider()
                    InfoRow(icon: "person.crop.circle", label: t(.account, L), value: s.account)
                    if !s.certNotAfter.isEmpty {
                        certRow(s.certNotAfter)
                    }
                }
            }
            if s.firefoxRunning {
                Callout(icon: "exclamationmark.triangle.fill", text: t(.firefoxOpen, L), tint: .orange)
            }
            VStack(spacing: 8) {
                Button { openANAF() } label: {
                    Label(t(.openAnaf, L), systemImage: "arrow.up.forward.app").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent).controlSize(.large).tint(Theme.brand)
                Button { Task { await doSetup(user: s.account) } } label: {
                    Label(t(.updateCert, L), systemImage: "arrow.clockwise").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered).controlSize(.large).disabled(busy)
            }
            HStack {
                if busy { ProgressView().controlSize(.small) }
                Spacer()
                Button(role: .destructive) { Task { await doUninstall() } } label: {
                    Text(t(.uninstall, L)).font(.callout)
                }
                .buttonStyle(.plain).foregroundStyle(.red).disabled(busy)
            }
        }
    }

    // A cert-expiry row whose value colours by remaining validity.
    private func certRow(_ iso: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "checkmark.shield").foregroundStyle(.secondary).frame(width: 16)
            Text(t(.validUntil, L)).foregroundStyle(.secondary)
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
            message = t(.setupDone, L); isError = false
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
            message = t(.uninstalled, L); isError = false
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
        case "firefox_running": return t(.errFirefox, L)
        case "no_credential":  return t(.errNoCred, L)
        case "no_profile":     return t(.errNoProfile, L)
        case "network":        return t(.errNetwork, L)
        default:               return r.error ?? t(.somethingWrong, L)
        }
    }

    private func parseDate(_ iso: String) -> Date? { ISO8601DateFormatter().date(from: iso) }

    private func formatDate(_ iso: String) -> String {
        guard let d = parseDate(iso) else { return iso }
        let f = DateFormatter()
        f.locale = Locale(identifier: localeID(L))
        f.dateStyle = .medium; f.timeStyle = .none
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
