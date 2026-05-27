# ADR — Markdown rendering on `apps/mobile/`

**Status**: Accepted
**Date**: 2026-05-19
**Supersedes**: nothing (formalises what `markdown-renderer-research.md` was
already documenting in research form)

This is the durable architecture-decision record for how the mobile app
renders markdown. `markdown-renderer-research.md` continues to hold the
detailed history and incident log; this file is the **one-page answer**
to "what are we using and why" with A-tier sources.

---

## Context — what the RN ecosystem actually offers (2026-05)

There are exactly three rendering paths available for markdown in React
Native today. Each has hard, library-independent constraints rooted in
the RN platform itself:

| Path | How it renders | Strengths | Hard limits |
|---|---|---|---|
| **A — Native** | md4c parses → iOS `NSAttributedString` / Android Spannable | Fastest, doesn't touch RN's nested-`<Text>` layout path | Cannot inject custom React for any leaf node (`enriched-markdown` issue [#54](https://github.com/software-mansion-labs/react-native-enriched-markdown/issues/54), [#232](https://github.com/software-mansion-labs/react-native-enriched-markdown/issues/232), maintainer: "no custom renderers, by design") |
| **B — React tree** | Parse to AST → walk → render every token as nested `<Text>` / `<View>` | Full custom React rendering for every node | Triggers RN's 10-year-old nested-`<Text>` bugs: [#10775](https://github.com/facebook/react-native/issues/10775), [#45925](https://github.com/facebook/react-native/issues/45925), [#6728](https://github.com/facebook/react-native/issues/6728) — `borderWidth` / `padding` / `margin` are not `NSAttributedString` attributes and either silently drop or force-break inline runs. CJK paragraphs amplify the symptom via UAX #14 / Kinsoku. |
| **C — WebView** | Web markdown lib (e.g. `react-markdown`) inside `react-native-webview` / Expo DOM Components | Identical to web output | Slower startup (no Hermes bytecode), async JSON-only bridge, native UI cannot embed inside the WebView, scroll & keyboard UX divergent. Expo's own docs acknowledge these are public trade-offs ([dom-components.mdx](https://docs.expo.dev/guides/dom-components/)) |

**Fact**: as of 2026-05, **no single library satisfies path A + path B
simultaneously** — i.e. native rendering performance AND custom React
component for arbitrary leaf nodes. This is an ecosystem-level constraint,
not a Multica problem.

### Concrete library survey (2026-05)

| Library | Path | Last release | Verdict for Multica |
|---|---|---|---|
| [`react-native-enriched-markdown`](https://github.com/software-mansion-labs/react-native-enriched-markdown) | A | v0.5.0 (Apr 2026) | **Selected for prose.** Expo officially recommends it in [Edit rich text](https://docs.expo.dev/guides/editing-richtext/) — A-tier endorsement. Software Mansion (same team as Reanimated / Gesture Handler) |
| [`react-native-streamdown`](https://github.com/software-mansion-labs/react-native-streamdown) | A + worklets | active 2026 | Not adopted. Built on enriched-markdown, optimised for AI streaming. Web/desktop don't use a streaming-specific renderer either, mobile streaming isn't currently a top product pain |
| [`react-native-marked`](https://github.com/gmsgowtham/react-native-marked) | B | v8.1.0 (2026-05-14) | Not adopted. v7 removed `CustomToken`, v8 added "React component embedding" but no token-level customisation. Pure `<Text>` tree → would trigger nested-text bugs |
| [`amilmohd155/react-native-markdown`](https://github.com/amilmohd155/react-native-markdown) | B | v0.8.5 (Jan 2026) | Not adopted. Same nested-`<Text>` constraint as `react-native-marked`. 14 ⭐, single maintainer, not production-validated |
| [`react-native-markdown-display`](https://www.npmjs.com/package/react-native-markdown-display) | B | ~2 years stale | Maintainer publicly recommends migrating away |
| Expo DOM Components (web `react-markdown` inside WebView) | C | Expo SDK 53+ stable | Not adopted as primary. Reserved as escape hatch for future LaTeX / Mermaid / very wide tables |
| [`vercel/streamdown`](https://github.com/vercel/streamdown) | n/a | active 2026 | Not applicable — name collides with SM's RN library but Vercel's `streamdown` is **web-only** (Next.js, AI SDK). Documented here only to dispel confusion |

Closed-source production references like Stream Chat's [`StreamingMessageView`](https://getstream.io/blog/react-native-assistant/) and [assistant-ui](https://www.assistant-ui.com/blog/2026-03-launch-week) do not publish their RN markdown implementation. There is **no public industry standard**.

---

## Decision

Multica mobile uses a **segment-based hybrid renderer** that dispatches
each markdown token type to the renderer that doesn't trip on that
token's specific platform trap.

```
content (string)
  ↓ preprocessMobileMarkdown
     legacy mention shortcode  →  [@name](mention://type/id)
     legacy file-card lines    →  [📎 name](url)
     HTML <br>                 →  CommonMark "  \n" hard break
  ↓ splitMarkdown                 (marked@18 lexer; we use the AST only)
     code fence          → { type:'code',  lang, code }
     paragraph w/ image  → image promoted to block, text rejoins prose
     everything else     → { type:'prose', content: token.raw }
  ↓ render per-segment
     prose  → <EnrichedMarkdownText>   path A
     code   → <CodeBlock>              path B (controlled — no CJK mixing)
     image  → <MarkdownImage>          path B (controlled — single element)
```

Each per-segment routing decision avoids the failure mode of the path it
chose:

| Segment | Routing | Trap avoided |
|---|---|---|
| Prose (paragraphs, headings, lists, quotes, tables, inline code, links, strong/em) | enriched-markdown (path A) | Would otherwise need React tree → CJK + inline-code chip = 5-7 line breakage per chip (the 2026-05-09 incident) |
| Fenced code block | own `CodeBlock` with one `<Text>` per line, token spans nested but **content is code only — no CJK paragraphs to trigger UAX #14**. Shiki for highlighting | Native rendering can't expose Shiki tokens; React tree of code-only doesn't trigger the CJK amplification of the nested-text bug |
| Image | `expo-image` wrapped in `Pressable` for lightbox dispatch. **One element, no nested-text mixing** | RN `<Image>` can't be inline in `<Text>`; lightbox needs `Pressable` not addressable inside attributed string |
| (future) LaTeX / Mermaid | not yet — when needed, separate component running Expo DOM Components | path C is the only one that gets these for free, but the WebView penalty isn't worth paying for prose |

### Marked@18 is used as a **lexer only**

`marked.lexer(input)` produces a token list. We never feed `marked`'s
HTML output to anything. `marked` is a 10-year-old, A-tier-maintained
CommonMark/GFM lexer ([marked.js docs](https://marked.js.org/)), and
running it as a pure JS function on every markdown body is cheap.

This is necessary because enriched-markdown's internal md4c AST is not
exposed — we'd have no way to find segment boundaries otherwise.

### Theming

Colors flow from the RNR design system:

- `global.css` defines CSS variables under `:root` (light) and
  `.dark:root` (dark)
- `lib/theme.ts` mirrors these as pre-resolved `hsl(...)` strings
  (CSS variable syntax doesn't work in RN imperative style objects)
- `lib/use-color-scheme.ts` is the single source of truth for the
  current scheme, persisted in `expo-secure-store`

For prose (path A, must use imperative style object — enriched is native
md4c, no className support), `useMarkdownStyle()` derives the full style
object from `THEME[scheme]`. For non-prose (paths B controlled), all
container styling uses NativeWind className like the rest of RNR.

### enriched-markdown's hidden-default trap (documented for posterity)

enriched-markdown's `normalizeMarkdownStyle.js` carries a frozen table of
~30 hardcoded **light-mode** color defaults. Fields not explicitly
overridden in `useMarkdownStyle()` use those hardcoded values and
disappear (or render garishly) in dark mode. Every color field must be
explicitly mapped to a `THEME[scheme]` token. **When upgrading
enriched-markdown (v0.6+), re-audit `normalizeMarkdownStyle.js` for
newly-added color fields** — they will also ship light-mode defaults.

---

## Consequences

### What we get

- Native attributed-string performance for the 95% case (prose)
- Web-parity syntax highlighting (Shiki, same themes as web)
- Image lightbox with native `expo-image` caching
- Full GFM support via enriched-markdown's `flavor="github"`
- Light / dark mode that follows `lib/use-color-scheme`
- Expo's own A-tier recommendation as our prose engine

### What we pay

- Three rendering paths to maintain instead of one
- Theme integration: every enriched color field must be explicitly mapped;
  hidden-default trap re-emerges on every enriched upgrade
- Code blocks nested in a list item stay with the enriched prose stream
  (don't get Shiki) — top-level code is the >95% case, acceptable
- LaTeX / Mermaid not currently supported

### Known limitations and mitigations

**Inline code chip top-heavy padding** — visible as `~13pt empty space
above` vs `~3pt below` glyphs in chips inside CJK paragraphs (seen in
#MUL-2397 and #MUL-2395 dark screenshots, 2026-05-19).

- **Root cause**: enriched-markdown applies hardcoded internal padding
  to inline code that cannot be turned off via `markdownStyle.code`. The
  `CodeStyle` schema does not expose `padding*` / `baselineOffset` /
  `lineHeight` knobs.
- **Not an RN/iOS platform issue**: Discord, Slack, Telegram, Mattermost
  mobile all render inline code with background + monospace and **do
  not** show this asymmetry — confirming the artifact is library-specific.
- **Upstream tracking**: [`software-mansion-labs/react-native-enriched-markdown#255`](https://github.com/software-mansion-labs/react-native-enriched-markdown/issues/255)
  (filed 2026-04-20 by `@xindixu`, maintainer unresponsive as of 2026-05-19).
- **Failed mitigation (reverted)**: reducing `MD_LINE.body` from 24 to
  20 shrinks absolute padding but does not change the asymmetry ratio —
  net negative (cost CJK leading, didn't fix the chip). See
  `markdown-renderer-research.md` decision log 2026-05-19.

**Mitigation applied (2026-05-19)** — inline code rendered WITHOUT a
background:

```ts
code: {
  color: t.brand,
  backgroundColor: "transparent",
  borderColor: "transparent",
  fontFamily: MONO_FONT,  // Menlo on iOS, monospace on Android
},
```

- `backgroundColor: "transparent"` — enriched still paints the padding
  rectangle internally, but it's invisible, so the top-heavy artifact
  disappears. Glyph baselines are unaffected (baseline is a font-metric
  property, not a background-painting property).
- `fontFamily: MONO_FONT` — enriched's native default for `code` is `''`
  (inherit from paragraph), so without this override mobile inline code
  would lose its only visual identity once the chip is removed.
- `color: t.brand` — secondary identification tint, distinguishes inline
  code from regular prose alongside the monospace.
- **Visual trade-off**: mobile no longer matches web/desktop chip style.
  Inline code on mobile reads as "tinted monospace span". Acceptable
  given that the alternative is the top-heavy chip artifact.
- **Revisit when**: upstream issue #255 ships a padding control. At
  that point switch back to a tinted-background chip for cross-platform
  parity.

**Why we did NOT** fork the library or rewrite the prose layer to a
React-tree renderer:

- Forking enriched-markdown means maintaining a native-code (ObjC/Swift
  + Kotlin) patch indefinitely; the ROI for one styling fix is poor.
- Rewriting the prose layer to a React-tree renderer (e.g.
  `react-native-marked`) would re-introduce the RN nested-`<Text>`
  platform bugs documented above — same root cause as the 2026-05-09
  inline-code CJK line-breakage incident.

### What's explicitly out of scope

- **Replacing the whole stack with a single library**: every alternative
  surveyed above either drops path A (perf) or drops custom React (lightbox /
  syntax highlight). No path forward there until the ecosystem ships a
  library that satisfies both.
- **Migrating chat to streamdown**: web/desktop have no streaming-specific
  renderer either; mobile parity demands the same. Reconsider only if
  AI-chat streaming becomes a top user complaint.

---

## When to revisit this ADR

- enriched-markdown ships custom React leaf-node rendering (currently
  not on roadmap — roadmap addresses `EnrichedMarkdownTextInput`, the
  *editor*, not the *renderer*)
- A new library appears that satisfies path A + path B simultaneously
- Expo SDK ships a first-party markdown renderer (currently doesn't)
- The product team commits to LaTeX / Mermaid as core features — Expo
  DOM Components becomes the right answer for that surface

---

## Sources (A-tier only)

### Official documentation

- [Expo — Edit rich text guide](https://docs.expo.dev/guides/editing-richtext/) — directly recommends `react-native-enriched-markdown`
- [Expo — Using React DOM in Expo native apps](https://docs.expo.dev/guides/dom-components/) — DOM Components trade-offs (path C)

### Library sources (maintainer-authoritative)

- [`software-mansion-labs/react-native-enriched-markdown`](https://github.com/software-mansion-labs/react-native-enriched-markdown) — path A primary
- [`software-mansion-labs/react-native-streamdown`](https://github.com/software-mansion-labs/react-native-streamdown) — surveyed, not adopted
- [`gmsgowtham/react-native-marked`](https://github.com/gmsgowtham/react-native-marked) — path B surveyed
- [`amilmohd155/react-native-markdown`](https://github.com/amilmohd155/react-native-markdown) — path B surveyed
- [`vercel/streamdown`](https://github.com/vercel/streamdown) — web only, documented to dispel naming collision
- [marked.js documentation](https://marked.js.org/) — lexer we use
- [Shiki](https://shiki.style/) + [`react-native-shiki-engine`](https://www.npmjs.com/package/react-native-shiki-engine) — code highlighting
- [`expo-image`](https://docs.expo.dev/versions/latest/sdk/image/) + [`jobtoday/react-native-image-viewing`](https://github.com/jobtoday/react-native-image-viewing) — image rendering
- [md4c](https://github.com/mity/md4c) — the C library that backs enriched-markdown on native

### Platform constraint sources (the "why we can't just use path B everywhere")

- [`facebook/react-native#10775`](https://github.com/facebook/react-native/issues/10775) — nested-`<Text>` border ignored (Nov 2016, locked, no fix)
- [`facebook/react-native#45925`](https://github.com/facebook/react-native/issues/45925) — same bug re-filed, still open under New Architecture
- [`facebook/react-native#6728`](https://github.com/facebook/react-native/issues/6728) — `margin` / `padding` ignored on nested `<Text>`
- [`react-native-community/discussions-and-proposals#695`](https://github.com/react-native-community/discussions-and-proposals/issues/695) — official statement on inline-text styling limits

### Reference implementations (same-pattern peers)

- [Mattermost mobile — `app/components/markdown/`](https://github.com/mattermost/mattermost-mobile/tree/main/app/components/markdown) — same segment-dispatch pattern, different engines
- Stream Chat [`StreamingMessageView`](https://getstream.io/blog/react-native-assistant/) — closed-source, recorded only as evidence that "no public standard exists"
- [assistant-ui multi-platform launch](https://www.assistant-ui.com/blog/2026-03-launch-week) — closed-source

### In-repo cross-references

- `apps/mobile/lib/markdown/markdown.tsx` — entry point
- `apps/mobile/lib/markdown/split-markdown.ts` — segment splitter
- `apps/mobile/lib/markdown/markdown-style.ts` — `useMarkdownStyle()` theme bridge
- `apps/mobile/lib/markdown/code-block.tsx` — Shiki-powered code segment
- `apps/mobile/lib/markdown/markdown-image.tsx` — lightbox-aware image segment
- `apps/mobile/docs/markdown-renderer-research.md` — full incident log and historical context
- `apps/mobile/CLAUDE.md` — mobile-wide rules including theme/CSS-variable system
