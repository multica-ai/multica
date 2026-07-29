# Decision: Mobile renders markdown by routing each segment to the path that survives it

Status: implemented

## Problem

`apps/mobile/` has to render the same markdown that web and desktop write into issue descriptions and comments, including tappable `@member` / `@agent` / `#issue` mention chips, inline images that open a lightbox, and `!file[name](url)` references that open through the system.

React Native offers exactly three rendering paths, and each has a hard limit that comes from the platform rather than from any library:

| Path | How it renders | Hard limit |
|---|---|---|
| A — native | md4c parses into `NSAttributedString` / Spannable | Cannot inject custom React for a leaf node; the maintainer of the leading library states this is by design |
| B — React tree | Parse to an AST, render every token as nested `<Text>` / `<View>` | Hits React Native's long-standing nested-`<Text>` bugs: `borderWidth`, `padding`, and `margin` are not attributed-string attributes, so they silently drop or force-break inline runs. CJK paragraphs amplify this through UAX #14 line breaking |
| C — WebView | A web markdown library inside a WebView or Expo DOM Component | Slower start with no Hermes bytecode, an async JSON-only bridge, no native UI embedded inside, and divergent scroll and keyboard behavior |

No library satisfies A and B at once — native rendering performance *and* custom React for arbitrary leaf nodes. That is an ecosystem constraint, and the closed-source production apps in this space do not publish their implementations, so there is no industry default to copy.

The cost of ignoring it is concrete: a React tree rendering a CJK paragraph containing an inline-code chip broke into five to seven lines per chip.

## Decision

Mobile uses a segment-based hybrid renderer, in `apps/mobile/lib/markdown/`. Each markdown token type is dispatched to the path that does not trip on that token's specific trap.

```
content (string)
  ↓ preprocessMobileMarkdown
       legacy mention shortcode → [@name](mention://type/id)
       legacy file-card lines   → [📎 name](url)
       HTML <br>                → CommonMark hard break
  ↓ splitMarkdown
       code fence          → { type: 'code', lang, code }
       paragraph w/ image  → image promoted to a block, text rejoins prose
       everything else     → { type: 'prose', content: token.raw }
  ↓ render per segment
       prose → EnrichedMarkdownText   (path A)
       code  → CodeBlock              (path B, controlled)
       image → MarkdownImage          (path B, controlled)
```

Prose — paragraphs, headings, lists, quotes, tables, inline code, links, emphasis — goes to `react-native-enriched-markdown`, which Expo recommends and Software Mansion maintains. It is native, so it avoids the nested-text bug entirely, which is what the CJK-plus-inline-code case needs.

Fenced code goes to a React tree deliberately, because native rendering cannot expose Shiki's token spans. A code block contains code, not CJK prose, so it does not trigger the line-breaking amplification that makes path B dangerous elsewhere. Images go to a React tree for the opposite reason: one `expo-image` element inside a `Pressable`, with no nested text to mix, because `<Image>` cannot be inline in `<Text>` and a lightbox needs a pressable that an attributed string cannot address.

`marked` is used as a lexer only. `marked.lexer(input)` returns a token list and its HTML output is never used. This is necessary because enriched-markdown's internal md4c AST is not exposed, so there is no other way to find segment boundaries.

Colors come from the RNR design system described in [the mobile UI foundation record](2026-05-20-mobile-rnr-ui-foundation.md). Prose must use an imperative style object, since enriched is native and takes no `className`, so `useMarkdownStyle()` derives the whole object from `THEME[scheme]`. The controlled paths style their containers with NativeWind classes like the rest of the app.

`normalizeMarkdownStyle.js` inside enriched-markdown carries a frozen table of roughly 30 hardcoded light-mode color defaults. Any field not explicitly overridden in `useMarkdownStyle()` takes one of those and either disappears or glares in dark mode, so every color field is mapped explicitly. Re-audit that file when upgrading enriched-markdown — new color fields arrive with light-mode defaults too.

## Alternatives considered

**One React-tree renderer for everything (`react-native-marked`, `amilmohd155/react-native-markdown`, `react-native-markdown-display`).** Rejected. All three render pure `<Text>` trees, which is precisely the configuration that broke CJK paragraphs containing inline code. Beyond that, `react-native-marked` v7 removed token-level customization and v8 added component embedding without restoring it; the third library is two years stale and its own maintainer recommends migrating away.

**One native renderer for everything.** Rejected, because it cannot produce a tappable mention chip, a lightbox-opening image, or Shiki-highlighted code — all four of the product requirements need custom React somewhere.

**A WebView or Expo DOM Component for everything.** Rejected as the primary path. Expo's own documentation acknowledges the startup, bridge, and interaction costs, and paying them on every comment body to serve prose is the wrong trade. It stays available as the escape hatch for LaTeX, Mermaid, and very wide tables, which path C gets for free.

**`react-native-streamdown`.** Not adopted. It builds on the same native engine but optimizes for AI streaming, which web and desktop do not use a specialized renderer for either, and streaming is not currently a mobile pain point.

## Consequences

Three rendering paths are maintained instead of one, and the theme integration has to map every enriched color field explicitly, with the hidden-default trap resurfacing on each upgrade.

A code block nested inside a list item stays in the enriched prose stream and does not get Shiki highlighting. Top-level code is well over 95% of real content, so this is accepted.

What it buys: native attributed-string performance for the prose that dominates real content, syntax highlighting at parity with web through the same Shiki themes, an image lightbox backed by `expo-image` caching, full GFM through enriched's GitHub flavor, and light and dark mode following the app's color scheme.

LaTeX and Mermaid are not supported.
