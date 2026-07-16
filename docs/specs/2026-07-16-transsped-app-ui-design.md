# TransSped — native app window (SwiftUI + Go engine) — Design Spec

**Date:** 2026-07-16
**Status:** Approved for planning
**Supersedes:** the one-shot osascript-dialog behavior of `cmd/tscloud-app`

---

## 1. Goal & scope

Today `TransSped.app` is a Go one-shot: it runs, shows a couple of `osascript`
dialogs, and exits — there is no lasting UI. Replace that with a **small native
window** that shows real status and offers the core actions, so launching the
app presents something meaningful instead of flashing and closing.

**In scope:**
- A SwiftUI window with two states: **not-set-up** (enter userID → set up) and
  **set-up** (status + actions).
- Actions: **Set up / Update**, **Open ANAF login**, **Uninstall**, **About**.
- Real status: installed-in-Firefox, account, certificate expiry, and
  warning conditions (module not registered, Firefox running).

**Out of scope (YAGNI):** multi-credential picker (this account has one), a
menu-bar/agent mode, auto-update, and any change to the PKCS#11 dylib or the
PIN/OTP sign-time flow (those are proven and untouched).

## 2. Architecture

`TransSped.app` becomes a SwiftUI front-end driving the existing Go logic as a
headless **engine**. Three payloads in the bundle:

| Bundle path | What | Language |
|---|---|---|
| `Contents/MacOS/TransSped` | SwiftUI window (the app executable) | Swift |
| `Contents/Resources/tscloud-engine` | status/setup/uninstall logic | Go |
| `Contents/Resources/libtscloud-pkcs11.dylib` | PKCS#11 module (unchanged) | Go |

The SwiftUI app shells out to `tscloud-engine` (via `Process`, resolved from
`Bundle.main.resourceURL`) and renders its JSON output. **All proven logic —
CSC cert fetch, Firefox `pkcs11.txt` registration, Keychain access, uninstall —
stays in Go and keeps its unit tests.** SwiftUI stays thin (rendering + input).

Rationale for the split: the fiddly, security-sensitive work (writing Firefox's
profile, `security` Keychain calls) already works in Go and dodges sandbox
issues by running as a separate process; SwiftUI only needs to present it.

The **engine is the evolution of `cmd/tscloud-app`** (renamed to
`cmd/tscloud-engine`): the `osascript` prompt/notify/confirm/fail helpers are
removed (the window replaces them); the file/profile/keychain functions are
kept as-is; a JSON command interface is added.

## 3. Engine interface (Go → SwiftUI contract)

The engine is invoked with a subcommand and always emits a single JSON object
on stdout. Non-fatal problems are reported in the JSON; a non-zero exit code
accompanies hard failures, with the same JSON error body.

**`tscloud-engine status`**
```json
{
  "installed": true,            // ~/.config/tscloud/config.json exists (cert fetched)
  "account": "+905415348385",   // config.userID
  "credentialID": "4274E027…",
  "label": "Trans Sped Cloud",
  "certNotAfter": "2027-05-12T09:14:00Z",  // parsed from leaf.der; "" if none
  "certSubject": "CN=…",
  "moduleRegistered": true,     // default Firefox profile's pkcs11.txt names our module
  "firefoxRunning": false,
  "firefoxProfile": "/Users/…/Profiles/xxxx.default-release"
}
```
`installed` (cert present) and `moduleRegistered` (Firefox knows the module) are
independent, so the UI can distinguish "cert fetched but not wired into Firefox"
from a clean install.

**`tscloud-engine setup --user <id>`** — fetch cert + register module. Emits
`{"ok":true,"message":"…","status":{…}}` (status re-embedded) or, on failure,
`{"ok":false,"error":"…","code":"…"}` with a machine-readable `code` so the UI
can tailor the message. Codes: `firefox_running`, `no_credential`, `no_profile`,
`network`, `unknown`.

**`tscloud-engine uninstall`** — `{"ok":true,"message":"…","notes":[…]}`;
unregisters the module, clears the Keychain PIN, deletes `~/.config/tscloud`.

Registration/uninstall require Firefox to be **closed**; if it is running the
engine returns `code:"firefox_running"` without modifying anything.

## 4. UI states & content

**Not set up** (`installed` false): a "Set up TransSped" card — a userID
`TextField` (placeholder "email or phone registered with Trans Sped") and a
**Set up** button. This is the native replacement for the old osascript userID
prompt. On success the window switches to the set-up state.

**Set up:**
- **Header:** app icon, "TransSped", version (from `Bundle` short version).
- **Status rows:**
  - ● **Installed in Firefox** (green) — or ⚠️ **Not registered in Firefox** (amber) if `moduleRegistered` is false.
  - **Account** — the userID.
  - **Certificate valid until** — `certNotAfter` formatted for the locale; turns amber if within 30 days, red if past.
  - ⚠️ **Firefox is open** row — shown only when `firefoxRunning` and an action needs it closed.
- **Buttons:** **Update** (re-run setup with the saved userID) · **Open ANAF login** (`open -a Firefox https://www.anaf.ro/`) · **Uninstall** (confirmation, then engine uninstall).
- **About** (sheet): one-line description; "Signing is delegated to the Trans Sped cloud — no private key is ever stored on this Mac."; version; GitHub link (`github.com/guness/transsped-mac`); license.

**Feedback:** a spinner + status line while the engine runs (setup has cloud
round-trips — a few seconds). Engine errors render inline in the card, mapped
from `code` to a friendly sentence (e.g. `no_credential` → "No certificate was
found for this userID — check it, or you may still need to enroll with Trans
Sped."; `firefox_running` → "Please quit Firefox first, then try again.").

## 5. Build / sign / notarize

`scripts/build-app.sh` gains:
- `swiftc -O -framework SwiftUI -framework AppKit -o "$APP/Contents/MacOS/TransSped" app/*.swift` (Swift sources live under `app/`; no Xcode project — the build stays script-driven).
- `go build -o "$APP/Contents/Resources/tscloud-engine" ./cmd/tscloud-engine`.
- The existing dylib build, icon (`AppIcon.icns`), and `Info.plist` (with `CFBundleExecutable = TransSped`) are unchanged.

Signing is unchanged in shape — **inner-out**: sign the dylib, the engine, the
app executable, then the bundle, with the Developer ID when `SIGN_ID` is set
(hardened runtime + timestamp) else ad-hoc. `scripts/make-dmg.sh` (DMG sign +
notarize + staple) is unchanged. Same notarized DMG output.

Prerequisite note: building now needs the Swift toolchain (Xcode Command Line
Tools, already required for the cgo dylib build) — no new install.

## 6. Error handling

- The engine never partially mutates on the `firefox_running` path — it checks
  first and returns the code, so the UI can prompt "quit Firefox" cleanly.
- All engine failures produce a JSON body with `error` + `code`; the SwiftUI
  layer maps `code` to a message and never shows a raw Go error string.
- If the engine binary is missing/unreadable (should never happen in a signed
  bundle), the app shows a single "installation looks corrupted — reinstall"
  message.

## 7. Testing

- **Go engine:** keep existing unit tests (register/unregister round-trip,
  config). Add a test for `status` JSON assembly (given a temp config + profile)
  and that `setup` refuses when Firefox is "running" (injected check).
- **SwiftUI:** kept thin, so logic coverage lives in the Go tests; the window
  itself is verified by a manual smoke run (launch → set-up state renders →
  Update/About work) documented in `scripts/smoke-test.md`.

## 8. Future (noted, not built)

Multi-credential picker (native list when `credentials/list` returns >1),
menu-bar mode, and in-app cert-renewal reminders — all straightforward to add
on this structure later.
