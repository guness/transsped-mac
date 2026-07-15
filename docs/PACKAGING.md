# Packaging & distribution

For your own machine, the default ad-hoc build is all you need:

```bash
./scripts/build-app.sh          # -> "TransSped.app"
```

An ad-hoc-signed app opens fine on the Mac that built it. To hand it to
**someone else** without them fighting Gatekeeper, you need a Developer ID
signature plus notarization.

## What you need to share it

1. **An Apple Developer account** (the paid Developer Program, $99/yr) — the
   free account cannot issue a Developer ID.
2. **A "Developer ID Application" certificate.** Create it in Xcode
   (Settings → Accounts → Manage Certificates → + → *Developer ID
   Application*) or on the Apple Developer portal, then confirm it's in your
   keychain:
   ```bash
   security find-identity -v -p codesigning | grep "Developer ID Application"
   ```
   > Note: an *Apple Development* or *iPhone Distribution* identity is **not**
   > sufficient — notarization specifically requires *Developer ID
   > Application*.
3. **Stored notarytool credentials** (one-time), using an
   [app-specific password](https://support.apple.com/en-us/HT204397):
   ```bash
   xcrun notarytool store-credentials easysign-notary \
     --apple-id "you@example.com" --team-id "TEAMID" --password "abcd-efgh-ijkl-mnop"
   ```

## Build → notarize → distribute

```bash
# 1. Developer ID build (hardened runtime + secure timestamp)
SIGN_ID="Developer ID Application: Your Name (TEAMID)" ./scripts/build-app.sh

# 2. Package into a DMG and notarize + staple
AC_PROFILE=easysign-notary ./scripts/make-dmg.sh
```

`make-dmg.sh` produces `TransSped.dmg` (with an Applications
drop-target), submits it to Apple's notary service, waits for the result, and
staples the ticket to both the DMG and the app. The stapled DMG opens on any
Mac with no Gatekeeper warning.

Without `SIGN_ID`/`AC_PROFILE` both scripts still run — they just produce an
ad-hoc, un-notarized build and print how to upgrade it.

## Verifying

```bash
codesign -dv --verbose=4 "TransSped.app"   # identity + hardened runtime flags
spctl -a -vvv --type exec "TransSped.app"  # Gatekeeper assessment (accepted = notarized)
xcrun stapler validate "TransSped.dmg"     # ticket is stapled
```
