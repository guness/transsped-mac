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
   xcrun notarytool store-credentials transsped-notary \
     --apple-id "you@example.com" --team-id "TEAMID" --password "abcd-efgh-ijkl-mnop"
   ```
   This project's profile is named **`transsped-notary`** and is already stored
   in the maintainer's Keychain.

## Build → notarize → distribute

```bash
# Pass SIGN_ID to both scripts. Use the identity name, or its SHA-1 (from
# `security find-identity -v`) to disambiguate if the cert is listed twice.
SIGN_ID="Developer ID Application: Your Name (TEAMID)"

# 1. Developer ID build (hardened runtime + secure timestamp)
SIGN_ID="$SIGN_ID" ./scripts/build-app.sh

# 2. Package into a DMG, sign it, notarize + staple
SIGN_ID="$SIGN_ID" AC_PROFILE=transsped-notary ./scripts/make-dmg.sh
```

`make-dmg.sh` produces `TransSped.dmg` (with an Applications drop-target),
code-signs the DMG with the Developer ID, submits it to Apple's notary service,
waits for the result, and staples the ticket to both the DMG and the app. The
result opens on any Mac with no Gatekeeper warning — even offline.

Without `SIGN_ID`/`AC_PROFILE` both scripts still run — they just produce an
ad-hoc, un-notarized build and print how to upgrade it.

## Verifying

```bash
codesign -dv --verbose=4 "TransSped.app"                              # identity + hardened runtime
spctl -a -vvv --type exec "TransSped.app"                            # app: accepted = notarized
spctl -a -vvv -t open --context context:primary-signature TransSped.dmg  # dmg: accepted
xcrun stapler validate "TransSped.dmg"                               # ticket is stapled
```

## Cutting a GitHub release

1. Bump the version in `scripts/build-app.sh` (`CFBundleShortVersionString`
   and `CFBundleVersion`), commit, and push `main`.
2. Build the signed, notarized DMG (the two commands above).
3. Tag and publish, attaching the DMG:
   ```bash
   VERSION=0.0.1
   git tag -a "v$VERSION" -m "TransSped v$VERSION"
   git push origin "v$VERSION"
   gh release create "v$VERSION" TransSped.dmg \
     --repo guness/transsped-mac \
     --title "TransSped v$VERSION" \
     --notes-file docs/release-notes/v$VERSION.md
   ```
   (`TransSped.dmg` is git-ignored — it ships as a release asset, not in the
   repo.)
