import Foundation

/// Mirrors the bare JSON printed by `tscloud-engine status`.
struct EngineStatus: Codable {
    var installed = false
    var account = ""
    var credentialID = ""
    var label = ""
    var certNotAfter = ""
    var certSubject = ""
    var moduleRegistered = false
    var firefoxRunning = false
    var firefoxProfile = ""
}

/// Mirrors the result envelope printed by `tscloud-engine setup|uninstall`.
struct EngineResult: Codable {
    var ok = false
    var message: String?
    var error: String?
    var code: String?
    var status: EngineStatus?
    var notes: [String]?
}

/// Runs the bundled Go engine and decodes its JSON.
enum Engine {
    private static func binaryURL() -> URL? {
        Bundle.main.url(forResource: "tscloud-engine", withExtension: nil)
            ?? Bundle.main.resourceURL?.appendingPathComponent("tscloud-engine")
    }

    private static func run(_ args: [String]) async -> Data? {
        guard let bin = binaryURL() else { return nil }
        return await withCheckedContinuation { cont in
            let p = Process()
            p.executableURL = bin
            p.arguments = args
            let out = Pipe()
            p.standardOutput = out
            p.standardError = Pipe()
            p.terminationHandler = { _ in
                cont.resume(returning: out.fileHandleForReading.readDataToEndOfFile())
            }
            do { try p.run() } catch { cont.resume(returning: nil) }
        }
    }

    static func status() async -> EngineStatus? {
        guard let d = await run(["status"]) else { return nil }
        return try? JSONDecoder().decode(EngineStatus.self, from: d)
    }

    static func setup(user: String) async -> EngineResult {
        await envelope(["setup", "--user", user])
    }

    static func uninstall() async -> EngineResult {
        await envelope(["uninstall"])
    }

    private static func envelope(_ args: [String]) async -> EngineResult {
        guard let d = await run(args),
              let r = try? JSONDecoder().decode(EngineResult.self, from: d)
        else {
            return EngineResult(ok: false, error: "The engine did not respond.", code: "unknown")
        }
        return r
    }
}

/// CFBundleShortVersionString, e.g. "0.0.2".
func appVersion() -> String {
    (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "?"
}
