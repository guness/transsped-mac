import SwiftUI

// Visual language for the window: a single brand accent (drawn from the app
// icon's blue) plus reusable grouped-card, info-row, and callout building blocks
// so the UI reads as one cohesive, native macOS panel.
enum Theme {
    static let brand = Color(red: 0.18, green: 0.42, blue: 0.88) // #2F6BE0, the icon blue
}

// Card is a grouped surface: a subtly filled, hairline-bordered rounded panel.
// The fill is derived from `primary` so it reads correctly in light and dark.
struct Card<Content: View>: View {
    @ViewBuilder let content: () -> Content
    var body: some View {
        content()
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(14)
            .background(Color.primary.opacity(0.045), in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Color.primary.opacity(0.08)))
    }
}

// InfoRow is one labelled fact inside a Card: leading glyph, muted label, and a
// value pinned to the trailing edge with monospaced digits for tidy alignment.
struct InfoRow: View {
    let icon: String
    let label: String
    let value: String
    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(.secondary).frame(width: 16)
            Text(label).foregroundStyle(.secondary)
            Spacer(minLength: 12)
            Text(value).fontWeight(.medium).monospacedDigit()
        }
        .font(.callout)
    }
}

// Callout is a tinted attention banner (warnings, assurances) — visually distinct
// from the neutral status facts so it reads as "notice," not "another row."
struct Callout: View {
    let icon: String
    let text: String
    let tint: Color
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: icon).foregroundStyle(tint)
            Text(text)
                .fixedSize(horizontal: false, vertical: true) // wrap, don't truncate
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.callout)
        .padding(10)
        .background(tint.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
    }
}
