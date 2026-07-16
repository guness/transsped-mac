import Foundation

// AppLang is the user's language *choice*: follow the system, or force one.
// Persisted (via @AppStorage) under the app's UserDefaults so the Go module can
// read the same preference for the sign-time dialog.
enum AppLang: String, CaseIterable {
    case system, en, ro
}

// Lang is the *resolved* language actually rendered (only en or ro).
enum Lang { case en, ro }

// resolve maps a stored choice to the language to render: an explicit choice
// wins; "system" picks Romanian when the Mac's preferred language is Romanian.
func resolve(_ storedChoice: String) -> Lang {
    switch AppLang(rawValue: storedChoice) ?? .system {
    case .en: return .en
    case .ro: return .ro
    case .system:
        let pref = Locale.preferredLanguages.first ?? "en"
        return pref.hasPrefix("ro") ? .ro : .en
    }
}

// Every user-facing string in the window.
enum K {
    case aboutTip, engineErrTitle, engineErrBody, retry
    case setUpTitle, setUpBlurb, emailOrPhone, setUp
    case installed, notInstalled, account, validUntil, firefoxOpen
    case openAnaf, updateCert, uninstall
    case aboutDesc, aboutSecurity, close
    case errFirefox, errNoCred, errNoProfile, errNetwork, somethingWrong
    case setupDone, uninstalled
    case language, langSystem
}

// t returns the localized string for a key. English is the source; Romanian is
// hand-translated with correct diacritics.
func t(_ key: K, _ lang: Lang) -> String {
    let pair = table[key] ?? ("\(key)", "\(key)")
    return lang == .ro ? pair.1 : pair.0
}

private let table: [K: (String, String)] = [
    .aboutTip:        ("About TransSped", "Despre TransSped"),
    .engineErrTitle:  ("Couldn't run the setup engine.", "Nu s-a putut porni motorul de configurare."),
    .engineErrBody:   ("The app may be damaged — reinstall TransSped from the DMG.",
                       "Aplicația poate fi deteriorată — reinstaleaz-o din fișierul DMG."),
    .retry:           ("Retry", "Reîncearcă"),

    .setUpTitle:      ("Set up TransSped", "Configurează TransSped"),
    .setUpBlurb:      ("Enter the email or phone registered with Trans Sped for your cloud certificate.",
                       "Introdu emailul sau telefonul înregistrat la Trans Sped pentru certificatul din cloud."),
    .emailOrPhone:    ("email or phone", "email sau telefon"),
    .setUp:           ("Set up", "Configurează"),

    .installed:       ("Installed in Firefox", "Instalat în Firefox"),
    .notInstalled:    ("Not registered in Firefox", "Neînregistrat în Firefox"),
    .account:         ("Account", "Cont"),
    .validUntil:      ("Valid until", "Valabil până la"),
    .firefoxOpen:     ("Firefox is open — quit it before Update or Uninstall.",
                       "Firefox este deschis — închide-l înainte de a actualiza sau dezinstala."),

    .openAnaf:        ("Open ANAF login", "Deschide autentificarea ANAF"),
    .updateCert:      ("Update certificate", "Actualizează certificatul"),
    .uninstall:       ("Uninstall", "Dezinstalează"),

    .aboutDesc:       ("Log in to ANAF SPV from macOS Firefox using your Trans Sped cloud qualified certificate.",
                       "Autentifică-te în ANAF SPV din Firefox pe macOS folosind certificatul calificat Trans Sped din cloud."),
    .aboutSecurity:   ("Signing is delegated to the Trans Sped cloud — no private key is ever stored on this Mac.",
                       "Semnarea este delegată către cloud-ul Trans Sped — nicio cheie privată nu este stocată vreodată pe acest Mac."),
    .close:           ("Close", "Închide"),

    .errFirefox:      ("Please quit Firefox first, then try again.",
                       "Închide mai întâi Firefox, apoi încearcă din nou."),
    .errNoCred:       ("No certificate was found for this userID. Check it — or you may still need to enroll with Trans Sped (ANAF form 150).",
                       "Nu s-a găsit niciun certificat pentru acest utilizator. Verifică-l — sau este posibil să fie nevoie de înrolare la Trans Sped (formularul 150 ANAF)."),
    .errNoProfile:    ("No Firefox profile found. Launch Firefox once, then quit it and try again.",
                       "Nu s-a găsit niciun profil Firefox. Deschide Firefox o dată, apoi închide-l și încearcă din nou."),
    .errNetwork:      ("Couldn't reach the Trans Sped service. Check your connection and try again.",
                       "Nu s-a putut contacta serviciul Trans Sped. Verifică conexiunea și încearcă din nou."),
    .somethingWrong:  ("Something went wrong.", "A apărut o problemă."),

    .setupDone:       ("Setup complete.", "Configurare finalizată."),
    .uninstalled:     ("Uninstalled.", "Dezinstalat."),

    .language:        ("Language", "Limbă"),
    .langSystem:      ("System", "Sistem"),
]

// localeID is the Foundation locale used for locale-aware formatting (dates).
func localeID(_ lang: Lang) -> String { lang == .ro ? "ro_RO" : "en_US" }
