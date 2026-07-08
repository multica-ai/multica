# Markdown Renderer Research (RN / Expo)

**Date:** 2026-05-09 (rewritten — initial research had a wrong claim about
mention syntax, see *Decision Log* below).
**Scope:** `apps/mobile/` — choosing a markdown renderer for Multica iOS that
matches the web/desktop feature set.

**Target requirements:**

1. Render the markdown that web/desktop already write to the database
   (issue descriptions + comments) without losing semantic content.
2. Make `@member` / `@agent` / `#issue` mentions tappable chips.
3. Make `![alt](url)` images inline + tappable to a lightbox.
4. Make `!file[name](url)` file references tappable + open via the system.

---

## TL;DR

**Use [`react-native-marked`](https://github.com/gmsgowtham/react-native-marked) v8.0.1.** Wrap it behind a thin adapter under `apps/mobile/lib/markdown/`. Override the `link` and `image` renderers — that is *all* the custom rendering we need. Mention detection is a one-line URL prefix check (`mention://`), file detection is a preprocess pass that rewrites `!file[name](url)` to a normal markdown link with a `📎` prefix. Use `expo-image` for inline images and `react-native-image-viewing` for tap-to-lightbox.

---

## Critical correction from initial research

The initial draft of this doc claimed mentions were stored as bare `@user` / `#issue` patterns and that mobile needed a custom `marked` inline tokenizer extension to detect them. **This was wrong.**

The actual encoding (see `packages/views/editor/extensions/mention-extension.ts:73`):

```ts
return `[${prefix}${safeLabel}](mention://${type}/${id})`;
// emits e.g. [@naiyuan](mention://member/abc-123)
//           [MUL-123](mention://issue/issue-uuid)
```

Mentions are **already standard markdown links** with a custom URI scheme. So mobile does NOT need a custom tokenizer — overriding `Renderer.link()` to check `href.startsWith("mention://")` is enough. This makes the integration significantly simpler than the initial sketch.

---

## What features web actually emits

Web's read-only renderer (`packages/views/editor/readonly-content.tsx`) uses `react-markdown` + this plugin set:

- `remark-gfm` — tables, task lists, strikethrough, autolinks
- `remark-breaks` — single newline → `<br>`
- `remark-math` + `rehype-katex` — `$inline$` and `$$block$$` math
- `rehype-raw` — raw HTML pass-through
- `rehype-sanitize` — HTML sanitization
- `lowlight` — code block syntax highlighting
- `mermaid` — diagrams (lazy loaded)

Plus a preprocess (`packages/views/editor/utils/preprocess.ts`):

1. Legacy mention shortcodes `[@ id="..." label="..."]` → modern `[@Label](mention://...)` form
2. Bare URL → markdown link (linkify)
3. File card syntax (`!file[name](url)` and legacy `[name](cdn-url)` lines) → HTML `<div data-type="fileCard">`

### Mobile coverage matrix

| Feature | Mobile V1 | Why |
|---|---|---|
| Bold / italic / strikethrough | ✅ | marked.js GFM, no extra cost |
| Inline `code` / fenced code blocks | ✅ | marked.js core; **no syntax highlight** in V1 (lowlight is ~200KB and not worth it) |
| Headings (h1-h6) | ✅ | core |
| Lists (ordered / unordered / **task**) | ✅ | GFM |
| Block quotes | ✅ | core |
| Plain links | ✅ | `Renderer.link` override → `Linking.openURL` |
| **Mention links** (`mention://...`) | ✅ | `Renderer.link` checks prefix → renders chip |
| **Inline images** (`![alt](url)`) | ⚠️ V2.2 | `Renderer.image` + expo-image. Defer to a separate phase to isolate risk |
| **Tap-to-lightbox** | ⚠️ V2.2 | `react-native-image-viewing`, single global lightbox provider |
| **File cards** (`!file[name](url)`) | ✅ | Preprocess to `[📎 name](url)`; ordinary link rendering, tap opens via system |
| Tables | ⚠️ V2.3 | marked emits tokens; we render simplified vertical layout. Phone screens can't fit wide tables |
| Autolinks | ✅ | GFM |
| `remark-breaks` (newline → break) | ✅ | marked.js `breaks: true` option |
| LaTeX math | ❌ V3+ | KaTeX is heavy + needs custom font shipping; rarely used |
| Mermaid | ❌ V3+ | webview-only on RN; very heavy; rarely used |
| Code syntax highlighting | ❌ V3+ | lowlight + theme CSS; nice-to-have not blocker |
| Raw HTML (`rehype-raw`) | ❌ never | Security boundary; mobile content all comes from web editor anyway |

---

## Library evaluation

| Library | Verdict | Notes |
|---|---|---|
| **`react-native-marked` v8.0.1** (2026-03-17) | **Pick** | Active (88 releases), TS 99.6%, RN 0.76+ compatible (we're on 0.83), peer dep `react-native-svg` already installed. Custom `Renderer` class + tokenizer extensions give us everything we need |
| `react-native-markdown-display` | Reject | Stale (last release ~2 years); maintainer recommends migrating away |
| `react-native-enriched-markdown` (Software Mansion) | Reject (now) | No custom inline component support yet (mention chips would need a roadmap feature). Revisit in 6-12 months |
| `react-native-awesome-gallery` (lightbox) | Reject | Last release 2024-07, 18+ months stale. Reanimated v3 dependency met but maintenance signal is bad |
| **`react-native-image-viewing`** (jobtoday) | **Pick for lightbox** | Pure TS, simple API (`<ImageView visible imageIndex images=[]/>`), zero animation deps |
| **`expo-image`** | **Pick for inline** | First-party Expo, on-disk cache, `contentFit` API, `transition` prop. Same engine the rest of the Multica stack will use for avatars later |

---

## Architecture

```
apps/mobile/
├── lib/markdown/
│   ├── index.ts                  # Public API: <Markdown content="..." />
│   ├── markdown.tsx              # Wraps react-native-marked w/ our renderer + preprocess
│   ├── renderer.tsx              # MulticaRenderer extends Renderer — overrides link, image
│   ├── preprocess.ts             # Mention-shortcode + file-card rewrite, idempotent
│   ├── mention-chip.tsx          # member / agent / issue chip components
│   ├── markdown-image.tsx        # expo-image + auto aspect ratio + tap-to-lightbox dispatch
│   └── lightbox-provider.tsx     # App-level provider + React Context for opening lightbox
└── components/issue/
    ├── comment-card.tsx          # <Text>{content}</Text> → <Markdown content={content}/>
    └── issue-description.tsx     # Same swap
```

The adapter boundary stays thin so swapping the engine later (e.g. once `react-native-enriched-markdown` matures) is a localized change.

---

## Mention rendering — the actual pipeline

```tsx
// lib/markdown/renderer.tsx (sketch)
class MulticaRenderer extends Renderer {
  link(text: string, href: string) {
    if (href.startsWith("mention://")) {
      const [, , type, id] = href.split("/");
      return <MentionChip key={this.getKey()} type={type} id={id} fallback={text} />;
    }
    return <PlainLink key={this.getKey()} href={href}>{text}</PlainLink>;
  }
  image(uri: string, alt?: string) {
    return <MarkdownImage key={this.getKey()} uri={uri} alt={alt} />;
  }
}
```

```tsx
// lib/markdown/mention-chip.tsx
function MentionChip({ type, id, fallback }: Props) {
  const { getName, getAvatarUrl } = useActorLookup();   // already exists in mobile
  const { issues } = useIssueListCache();                // mobile-local helper

  if (type === "member" || type === "agent") {
    const name = getName(type, id);
    return (
      <Text className="text-primary font-medium">@{name ?? fallback.replace(/^@/, "")}</Text>
    );
  }
  if (type === "issue") {
    const issue = issues.find((i) => i.id === id);
    const onPress = () =>
      router.push(`/${wsSlug}/issue/${id}`);
    return (
      <Text className="text-blue-500" onPress={onPress}>
        {issue?.identifier ?? fallback}
      </Text>
    );
  }
  return <Text>{fallback}</Text>;
}
```

The "miss → render fallback string" path matters: in production, member/agent lists are pre-loaded in `[workspace]/_layout.tsx` so cache hit rate is near 100%. The fallback string is the original markdown link text (`@naiyuan` / `MUL-123`) so the user always sees something readable.

---

## Image rendering — auto aspect ratio

```tsx
function MarkdownImage({ uri, alt }: { uri: string; alt?: string }) {
  const { open } = useLightbox();
  const [aspect, setAspect] = useState<number | null>(null);

  useEffect(() => {
    Image.getSize(
      uri,
      (w, h) => setAspect(w / h),
      () => setAspect(16 / 9), // fallback when remote 404 / decode fails
    );
  }, [uri]);

  return (
    <Pressable onPress={() => open(uri)}>
      <ExpoImage
        source={{ uri }}
        style={{ width: "100%", aspectRatio: aspect ?? 16 / 9, borderRadius: 8 }}
        contentFit="contain"
        transition={150}
      />
    </Pressable>
  );
}
```

Using `aspectRatio` rather than fixed `height` avoids layout shift once the image actually loads — the placeholder takes the right space from the start.

---

## File card rendering — preprocess into a normal link

```ts
// lib/markdown/preprocess.ts
const FILE_LINE_RE = /^!file\[([^\]]+)\]\((https?:\/\/[^)]+)\)$/;

export function preprocessMobileMarkdown(input: string): string {
  return input
    .split("\n")
    .map((line) => {
      const trimmed = line.trim();
      const m = trimmed.match(FILE_LINE_RE);
      if (!m) return line;
      return `[📎 ${m[1]}](${m[2]})`;
    })
    .join("\n");
}
```

Then `Renderer.link` opens the URL via `Linking.openURL` — iOS handles PDF/image/zip/etc. via system viewers or share sheet automatically. No custom file-card chip component needed in V1.

**Legacy CDN-hostname-based file detection (`[name](cdn-url)` on its own line)** is deliberately NOT ported. It depends on having `cdnDomain` from config; mobile doesn't bootstrap config that way. Old comments using the legacy form will render as a normal hyperlink — the tap behavior is identical (open in system), only the visual decoration differs. Acceptable degradation.

---

## Mention shortcode preprocess (legacy DB rows)

The DB has two mention serializations because of an April 2026 migration:

- **New**: `[@Label](mention://member/id)` — emitted by current Tiptap editor
- **Legacy**: `[@ id="abc-123" label="Naiyuan"]` — old shortcode form

Mobile must convert legacy → new before parsing, otherwise old comments render the literal shortcode text. Logic is a pure regex transform — `packages/ui/markdown/index.ts` exports `preprocessMentionShortcodes` for web/desktop, but mobile **cannot** import from `@multica/ui/*` (Sharing Principles in `apps/mobile/CLAUDE.md`).

Two options:

| Option | Trade-off |
|---|---|
| **A.** Lift the pure function to `@multica/core/markdown/` so all three apps share it | One PR adds a new core module; web/desktop migrate their import path; mobile imports same function. Single source of truth |
| **B.** Mobile re-implements it (~30 lines) | No web/desktop change; risk of drift if the legacy format ever expands |

**Recommend A.** This is exactly the kind of pure-function-share-zone the monorepo is for, and parity is required (same legacy comment must produce the same mention id on both clients).

---

## Lightbox

Single global lightbox provider mounted at `app/(app)/_layout.tsx`:

```tsx
function LightboxProvider({ children }: { children: ReactNode }) {
  const [images, setImages] = useState<string[]>([]);
  const [index, setIndex] = useState(0);
  const open = (uri: string) => { setImages([uri]); setIndex(0); };
  const close = () => setImages([]);
  return (
    <LightboxContext.Provider value={{ open }}>
      {children}
      <ImageView
        images={images.map((uri) => ({ uri }))}
        imageIndex={index}
        visible={images.length > 0}
        onRequestClose={close}
      />
    </LightboxContext.Provider>
  );
}
```

V1 only opens single images. V2 can extend to galleries (all images in one comment shown together) by collecting `![]()` URLs while rendering and passing the array through.

---

## Phasing

| Phase | Scope | Days |
|---|---|---|
| **V2.1** | preprocess (mention shortcodes + file cards) + plain markdown rendering (bold/italic/code/lists/quotes/headings/links/mention chips). Replaces `<Text>{content}</Text>` in comment-card and issue-description | ~1 |
| **V2.2** | Inline images via `expo-image` + lightbox via `react-native-image-viewing` | ~0.5 |
| **V2.3** | Tables (vertical degraded layout), task list checkboxes (read-only), strikethrough | ~0.5 |
| **V3+** | LaTeX, Mermaid, syntax highlighting — only when product asks for them | per-feature |

---

## Reference architectures

### Mattermost mobile (still relevant for image sizing + mention fallback patterns)

[`mattermost/mattermost-mobile`](https://github.com/mattermost/mattermost-mobile)'s pipeline is more complex than ours because they fork commonmark.js for four custom inline syntaxes. We need none of that — our mentions are already standard markdown links. What's still worth lifting from them:

- Image sizing: viewport-width-aware + max-height clamps + tap-to-gallery (we mirror this in `MarkdownImage`)
- Mention fallback: render the original token text on cache miss (we mirror this in `MentionChip`)

Source files worth glancing at before implementation:

- [`markdown_image/index.tsx`](https://github.com/mattermost/mattermost-mobile/blob/main/app/components/markdown/markdown_image/index.tsx) — viewport sizing, Android clamps
- [`at_mention/at_mention.tsx`](https://github.com/mattermost/mattermost-mobile/blob/main/app/components/markdown/at_mention/at_mention.tsx) — fallback render

We deliberately don't lift their `transform.ts` (heavy AST manipulations to make commonmark behave like their custom dialect) — our content is already normalized by Tiptap.

---

## RN native rendering constraints (May 2026)

This section is the **single source of truth** for what does and does not work
when styling markdown in React Native. Every rule below is backed by a
reproducible RN issue thread — these are not design trade-offs, they are
platform limits of how iOS TextKit + RN's nested-Text flattener interact.

### The nested-`<Text>` rule (the most important one)

**Inline `<Text>` runs nested inside another `<Text>` flatten into a single
`NSAttributedString` on iOS.** `NSAttributedString` runs only support font /
color / underline / strikethrough / shadow attributes. Any of the following
properties on an inline child `<Text>` are **silently ignored OR force the
run to break out of inline flow** (depending on RN version + New Arch):

- `borderWidth`, `borderColor`, `borderRadius`
- `padding*`, `margin*`
- `width`, `height`

This is **not a bug we can wait out** — RN has known about it for 10 years
and chosen not to fix it because it requires a new layout engine for inline
runs. Track:

- [`facebook/react-native#10775`](https://github.com/facebook/react-native/issues/10775) (Nov 2016, locked, no fix)
- [`facebook/react-native#6728`](https://github.com/facebook/react-native/issues/6728) (margin/padding ignored on nested Text)
- [`facebook/react-native#45925`](https://github.com/facebook/react-native/issues/45925) (Aug 2024, **still open**, reproduces under New Arch / Fabric)
- [`react-native-community/discussions-and-proposals#695`](https://github.com/react-native-community/discussions-and-proposals/issues/695) (Jul 2023): explicitly states *"React Native ignores padding on inline text elements. Border styles for inline text are not supported. If the inline text has a background color set, it fills the whole line."*

**On iOS New Arch / Fabric specifically, the breakage mode flipped from
"silently dropped" (Paper) to "force run break" (Fabric).** That is the
regression we hit shipping inline-code chips with `border` + `py-0.5` —
each chip pushed to its own line.

**CJK amplifies the symptom but is not a separate bug.** iOS TextKit uses
UAX #14 / Kinsoku rules that treat every CJK ideograph as a soft-break
opportunity. When a non-flowable inline span (border'd `<Text>`) sits in a
CJK paragraph, the break solver pessimistically breaks at every adjacent
ideograph boundary, exploding a single visual line into 6-7 broken lines.
Pure-Latin paragraphs would only look slightly ugly; mixed CJK paragraphs
look catastrophic. **Fix the underlying Text rule, not the symptom.**
([`facebook/react-native#19193`](https://github.com/facebook/react-native/issues/19193) tracks adjacent CJK baseline drift.)

### Workarounds that actually work — and the ones that don't

| Approach | Status |
|---|---|
| Inline `<Text>` with `bg + fontFamily` only | ✅ The native idiom. Mattermost / GitHub mobile / `react-native-marked` all do this for inline code |
| Inline `<Text>` with `bg + border + padding` | ❌ The bug. **Do not use, ever.** |
| Wrap chip in `<View>` | ❌ Becomes block-level — collapses paragraph flow, every chip on its own line by definition |
| `react-native-enriched-markdown` (Software Mansion) | Uses native attributed-string + inline-image hacks; works but no custom inline component slot — already rejected for V1 |
| Border-on-block-Text (e.g. fenced code block in a `<View>`) | ✅ Borders work fine on `<View>` and on **non-nested** root `<Text>` |

**Translation to web parity expectations:** web's "3-layer chip" (`bg + border + opacity`) is a CSS inline-block affordance that **has no faithful RN equivalent**. Mobile must compensate with a single-layer chip — bumped background opacity is the safe substitute for the missing border.

### `<Text lineBreakStrategyIOS>` for CJK

RN 0.71+ exposes `lineBreakStrategyIOS` to opt the outer paragraph `<Text>`
into the iOS-native CJK-aware break strategies (`"hangul-word"` /
`"push-out"`). Applying this on the outermost `<Text>` of a paragraph
visibly improves CJK wrapping at line edges. Set it on every paragraph
`<Text>` and every list-item content `<Text>`.

### Tiptap-emitted markdown serialization

[Tiptap's default `clipboardTextSerializer`](https://tiptap.dev/docs/editor/extensions/nodes/hard-break) emits `\n\n` between paragraphs and `\n` only for explicit `HardBreak` nodes (Shift+Enter). Bare single `\n` inside a paragraph is rare in Multica content (everything that lands in our DB went through tiptap). So `marked.lexer({ breaks: true })`:

- Is **harmless** for tiptap content.
- Is **defensive** for non-tiptap input (paste, IME, future API ingestion paths).
- Matches web's `remark-breaks` plugin, preserving cross-platform parity.

**Verdict: leave `breaks: true` on.** The exploding-paragraph symptom in
the May 2026 incident was the nested-Text border bug, not `breaks`.

### Mobile typography scale (verified against Apple HIG)

shadcn / web defaults are too large on phone screens. Calibrated against
Apple HIG Dynamic Type (Body 17pt, Title 3 20pt, Title 2 22pt, Title 1
28pt) and verified in GitHub mobile / Linear iOS:

| Element | Mobile class | Rationale |
|---|---|---|
| Body / paragraph | `text-sm leading-6` | 14px on 24px line ≈ 1.71 — generous for CJK glyph height; HIG Body Compact. See *Body lineHeight stays at 24* below for why a 2026-05-19 reduction attempt was reverted |
| Inline code | `text-sm font-mono` | Match body size, mono variant — chip identification is by bg, not size |
| Block code | `text-sm font-mono` | Same as inline |
| H1 | `text-xl font-bold` (20px) | HIG Title 3 — markdown H1 inside an issue card is structural, not a screen title |
| H2 | `text-lg font-semibold` (18px) | One step down from H1; matches GitHub mobile |
| H3 | `text-base font-semibold` (16px) | Body+1 |
| H4–H6 | `text-sm font-semibold` | Body, weight to differentiate |

**Body lineHeight stays at 24 (ratio 1.71) — 1.43 reduction reverted same day**

A 2026-05-19 attempt to reduce `MD_LINE.body` from 24 to 20 (ratio 1.71
→ 1.43) — to fix the inline-code chip's top-heavy visual artifact — was
**reverted on the same day** after additional research falsified the
hypothesis.

Falsifying evidence:

1. **Discord / Slack / Telegram / Mattermost mobile all use background +
   monospace** for inline code with NO visible top-heavy artifact —
   confirming the artifact is not an RN+iOS platform-wide problem.
2. **Upstream issue [#255](https://github.com/software-mansion-labs/react-native-enriched-markdown/issues/255)**
   on `react-native-enriched-markdown` (filed 2026-04-20) is the same
   problem reported by another user: enriched applies hardcoded inline-
   code padding that cannot be turned off via `markdownStyle`. Maintainer
   has not responded, no ETA.

So the chip is the **library's bug**, not a `lineHeight` configuration
problem. Reducing `lineHeight` shrinks the absolute padding but does
NOT change the top:bottom asymmetry ratio (still ≈ 4:1 either way),
because the ratio is set by enriched's internal padding distribution.
Reducing `lineHeight` only costs us CJK leading without solving the
artifact.

`leading-6` (1.71) restored. The original PingFang SC argument applies:
CJK glyphs are taller than SF and benefit from ≥1.5 leading.

**Accepted as a known limitation, tracked in
`docs/markdown-rendering-adr.md` → Known limitations.**

### List bullet column width

Canonical mobile pattern: 16px hanging indent column for `•` / `1.` —
`<View>` row of `<Text className="w-4">` for the bullet plus `<View
className="flex-1">` for content. GitHub mobile and Linear iOS both use
~16px. Bump to 20px (`w-5`) only for nested lists where deeper indents
need extra room.

`w-6` (24px, our pre-incident default) leaves visible dead space between
the bullet and the text.

### Block spacing between paragraphs

`mb-3` (12px) is the right default for the *general* block-to-block
transition. For consecutive single-line paragraphs in CJK, `mb-2` (8px)
reads tighter and better. We use a **single block gap** for simplicity —
12px between everything. Tightening adaptively is a polish item, not a
correctness one.

### NativeWind 4 specific notes

- Color opacity (`bg-foreground/10`) is computed at build time, not runtime — works fine on `<Text>`.
- `border` compiles to `borderWidth: 1, borderColor: ...`. NativeWind faithfully forwards both. RN drops them on nested `<Text>`. **Not a NativeWind bug — it's RN.**
- No NativeWind 4 docs acknowledge nested-Text styling limits — they inherit RN behavior silently. Don't assume a Tailwind class works on `<Text>` just because it works on `<View>`.

---

## Decision log

- **2026-05-09 (initial)** — Picked `react-native-marked`. Sketched a mention pipeline with custom marked tokenizer for `@user` / `#issue` patterns.
- **2026-05-09 (rewrite)** — Discovered `mention-extension.ts:73` actually emits markdown links with `mention://` URI scheme. Replaced the tokenizer-extension plan with a `Renderer.link` override (much simpler). Added image / file / lightbox plans now that the data shape is verified end-to-end. Confirmed `react-native-marked` and `react-native-svg` are still the right primitives in May 2026; chose `react-native-image-viewing` over `react-native-awesome-gallery` (the latter has been stale since 2024-07).
- **2026-05-09 (engine actually shipped)** — Replaced planned `react-native-marked` adapter with a hand-rolled walker on raw `marked@18` (`render-block.tsx` / `render-inline.tsx` / `ast.ts`, ~450 LOC). Reason: `react-native-marked` v7.0 (Jun 2025) removed the `CustomToken` API needed for inline mention chips; v8 added React-component embedding back but custom inline tokenization is still constrained. Walker keeps total control over mention chips, file cards, and the inline-image-promotion AST pre-pass.
- **2026-05-09 (Shiki + Expo Go assumption dropped)** — Project moved to dev client (custom native build) — Expo Go-compatible constraint no longer applies. Adopted `react-native-shiki-engine` (JSI Oniguruma + native engine) with reused web Shiki theme JSON (`github-light` / `github-dark`) for **byte-identical syntax highlighting** between web and mobile. Highlighter singleton lives in `lib/markdown/shiki.ts`, prewarmed at app boot.
- **2026-05-09 (inline code chip incident)** — Shipped 3-layer styling (`bg + border + opacity`) ported from web. CJK paragraphs catastrophically broke into 6-7 lines per chip. Root cause: RN's 10-year-old nested-Text limitation (issues #10775 / #45925 / #6728 — `borderWidth` / `padding` / `margin` are not part of `NSAttributedString`'s expressible attribute set, so iOS TextKit either drops them or breaks the run out of inline flow; New Arch / Fabric flipped the failure mode from "silent drop" to "force break"). CJK amplified the symptom because UAX #14 / Kinsoku treat every ideograph as a soft-break opportunity around the non-flowable run. Fix: drop border + padding from inline code, bump background opacity from `/5` to `/10` to compensate. **Encoded the rule in *RN native rendering constraints* above so this never happens again.**

- **2026-05-19 (RNR theme integration + enriched-markdown dark-default trap)** — Migrated `markdown-style.ts` from static hex constants to a `useMarkdownStyle()` hook driven by `THEME[scheme]` (from `lib/theme.ts`, mirroring `global.css` CSS variables). Non-prose layers (`CodeBlock` / `MarkdownImage`) were already className-driven and only needed dark-mode `--code-surface` (changed from light-mirror `240 4% 92%` to `240 4% 18%`). Two waves of dark-mode bugs uncovered the same root cause:

  **First wave** (visible breakage in `MUL-2395` dark screenshot): list items rendered as black-on-black (invisible); inline code showed a white-ish empty outline (no fill).

  **Root cause**: `react-native-enriched-markdown@0.5.0`'s `normalizeMarkdownStyle.js` defines `DEFAULT_NORMALIZED_STYLE` — a frozen table of ~30 hardcoded LIGHT-mode color defaults (`#1F2937` text, `#E01E5A` inline code, `#FDF2F4` inline code bg, `#F8D7DA` inline code border, `#4B5563` blockquote, `#FFFFFF` table even row, `#000000` checked task text, etc.). User `markdownStyle` is merged on top — **fields you don't pass keep the hardcoded light value**. There is no "inherit from parent" fallback at the native md4c layer.

  The "white outline" on inline code in dark mode was the default `#F8D7DA` (pale pink) border showing against `#0a0a0a` page background.

  **Fix**: read the entire `normalizeMarkdownStyle.js` file (~264 LOC), enumerate every color field, set every one explicitly from `THEME[scheme]`. Currently covered: `paragraph/h1–h6/blockquote/list/codeBlock/link/strong/em/strikethrough/underline/code/thematicBreak/table/taskList/math/inlineMath`. Also pass `lineHeight` explicitly on every heading because enriched's heading lineHeight defaults are calibrated to its larger default fontSizes (h1: 36pt for 30pt fontSize) — using mobile's smaller heading fontSizes (h1: 20pt) without overriding lineHeight produces awkwardly tall lines.

  **Rule to encode for future maintainers**:

  > **Any time `EnrichedMarkdownText`'s `markdownStyle` is extended, audit `normalizeMarkdownStyle.js` in `node_modules/react-native-enriched-markdown/lib/module/` and verify every color field has an explicit `THEME[scheme]` override. Do NOT assume "inherit". When upgrading enriched-markdown (v0.6+), re-run the audit — new fields ship hardcoded light defaults too.**

  Inline code background alpha: `12%` on dark is invisible against `#0a0a0a`; bumped to `40%` (`#9ca3af66`) in dark mode while keeping `12%` in light. Border forced to `transparent` to kill the default pink stroke.

- **2026-05-19 (lineHeight 24→20 attempted then REVERTED same day)** — Saw inline-code chip with very top-heavy padding in #MUL-2397 screenshot. Initial hypothesis: paragraph `lineHeight 24` + iOS TextKit descender-flush glyph positioning produced ~4.3:1 top:bottom space ratio. Reduced `MD_LINE.body` 24→20 to compress. **Reverted same day after research falsified the hypothesis**:
  - Discord / Slack / Telegram / Mattermost mobile all use background + monospace inline code with no visible top-heavy artifact ([Discord markdown guide](https://gist.github.com/matthewzring/9f7bbfd102003963f9be7dbcf7d40e51), [Slack docs](https://slack.com/help/articles/202288908-Format-your-messages), [Telegram entities API](https://core.telegram.org/api/entities)) — confirming the artifact is NOT a RN+iOS structural limitation
  - Upstream [`react-native-enriched-markdown#255`](https://github.com/software-mansion-labs/react-native-enriched-markdown/issues/255) — same user-reported problem, maintainer unresponsive — confirms enriched applies hardcoded inline-code padding that can't be turned off
  - Reducing `lineHeight` shrinks absolute padding but doesn't shift the asymmetry ratio (enriched's internal padding distribution is what sets the ratio). Net effect of the reduction: cost CJK leading, didn't fix the chip
  - Restored `MD_LINE.body: 24`. Heading values restored: h1 28, h2 24, h3 22, h4/h5 20, h6 18.
  - **Lesson**: when a chip artifact appears in enriched-markdown output, check upstream issues BEFORE proposing a numeric fix on our side. Chip layout is library-controlled, not consumer-controlled. Now tracked in `docs/markdown-rendering-adr.md` → Known limitations.

---

## References

### Libraries

- [`react-native-marked` — GitHub](https://github.com/gmsgowtham/react-native-marked) (v8.0.1, 2026-03-17)
- [`react-native-marked` — npm](https://www.npmjs.com/package/react-native-marked)
- [`marked.js` extension API](https://marked.js.org/using_pro)
- [`expo-image` — Expo docs](https://docs.expo.dev/versions/latest/sdk/image/)
- [`react-native-image-viewing` — GitHub](https://github.com/jobtoday/react-native-image-viewing)
- [`react-native-image-viewing` — npm](https://www.npmjs.com/package/react-native-image-viewing)
- [`react-native-enriched-markdown` — GitHub](https://github.com/software-mansion-labs/react-native-enriched-markdown) (rejected for V1: no custom inline component support)

### In-repo references

- `packages/views/editor/extensions/mention-extension.ts` — actual mention serialization
- `packages/views/editor/readonly-content.tsx` — web's read-only render plugin set
- `packages/views/editor/utils/preprocess.ts` — three-step preprocess pipeline web uses
- `packages/ui/markdown/file-cards.ts` — file-card detection logic (for parity reference)
- `apps/mobile/data/use-actor-name.ts` — mention name resolution helper (already implemented)

### Reference implementations

- [Mattermost mobile — `app/components/markdown/`](https://github.com/mattermost/mattermost-mobile/tree/main/app/components/markdown)
- [Rocket.Chat — `@rocket.chat/message-parser`](https://www.npmjs.com/package/@rocket.chat/message-parser)

### RN platform constraint sources (May 2026)

- [RN #10775 — "Border can't be applied to `<Text>` in nested `<Text>`"](https://github.com/facebook/react-native/issues/10775) — Nov 2016, locked, no fix
- [RN #45925 — Same bug re-filed, **still open**](https://github.com/facebook/react-native/issues/45925) — Aug 2024, reproduces under New Arch
- [RN #6728 — `margin` / `padding` ignored on nested `<Text>`](https://github.com/facebook/react-native/issues/6728)
- [discussions-and-proposals #695 — "Inline code blocks"](https://github.com/react-native-community/discussions-and-proposals/issues/695) — Jul 2023, official statement that nested-Text padding/border is unsupported
- [RN #19193 — Mixed CJK + Latin baseline drift](https://github.com/facebook/react-native/issues/19193) — adjacent CJK rendering quirk
- [RN Text component docs (current)](https://reactnative.dev/docs/text)
- [NativeWind 4 — Quirks](https://www.nativewind.dev/docs/core-concepts/quirks) (does not call out nested-Text limits — pure RN issue)
- [Tiptap — HardBreak node docs](https://tiptap.dev/docs/editor/extensions/nodes/hard-break) — clipboardTextSerializer behavior
- [Apple HIG — Typography](https://developer.apple.com/design/human-interface-guidelines/typography) — Dynamic Type sizing
- [Material 3 — Typography scale](https://m3.material.io/styles/typography/applying-type)
