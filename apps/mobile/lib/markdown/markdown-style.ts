/**
 * `markdownStyle` prop value for `EnrichedMarkdownText`. Driven by RNR
 * theme tokens (`apps/mobile/lib/theme.ts`, mirroring CSS variables in
 * `apps/mobile/global.css`) so colors track light/dark automatically.
 *
 * Why a hook instead of a static object: enriched-markdown is a native
 * (md4c → NSAttributedString / Spannable) layer that only accepts an
 * imperative style object — it can NOT consume NativeWind classNames.
 * The hook is the bridge: it reads the current colorScheme via the same
 * `useColorScheme` everything else in the app uses, and rebuilds the
 * style object whenever the theme flips.
 *
 * Sizing follows the mobile typography scale documented in
 * `apps/mobile/docs/markdown-renderer-research.md` → "Mobile typography
 * scale" (calibrated against Apple HIG; one tier below shadcn web defaults
 * because markdown headings inside an issue card are structural, not
 * screen titles). HIG values are encoded in `MD_FONT` / `MD_LINE` /
 * `MD_GAP` constants — these are NOT RNR tokens to replace; they are
 * mobile-specific design constants validated by the 2026-05-09 inline-
 * code incident.
 */
import { useMemo } from "react";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

/**
 * Typography scale — Apple HIG-calibrated, one tier below shadcn web.
 * See `docs/markdown-renderer-research.md` "Mobile typography scale".
 */
const MD_FONT = {
  body: 14,
  h1: 20,
  h2: 18,
  h3: 16,
  // h4 = 15 (was 14, same as body) — give h4 a real visual step over body
  // beyond just fontWeight. h5 stays at 14 so it can read as "bold body"
  // when authors use it for inline emphasis on a list section.
  h4: 15,
  h5: 14,
  // h6 = 13 (was 12) + foreground (was mutedForeground) — the prior config
  // rendered h6 as "small gray text" indistinguishable from a caption.
  h6: 13,
  // Code block font size — mirrors the in-house `CodeBlock` component's
  // `text-[13px]` (see `tokens.ts` → CODE_BLOCK_TEXT_CLASS). The two paths
  // MUST agree so top-level fenced code (rendered by CodeBlock) and
  // list-nested code (rendered by enriched via this style) look identical.
  // Both were lowered from 14 to 13 to match GitHub Mobile / Linear iOS /
  // Notion iOS — at the same point size mono is visually denser than
  // PingFang, and 14 read as "louder than body" inside a card.
  codeBlock: 13,
} as const;

const MD_LINE = {
  // Body lineHeight 24 on fontSize 14 = ratio 1.71. Generous for CJK
  // paragraph readability (PingFang SC glyphs are taller than SF and
  // benefit from ≥1.5 leading).
  //
  // A 2026-05-19 attempt to reduce this to 20 (ratio 1.43) — to "fix"
  // the inline-code chip's top-heavy padding — was REVERTED on the same
  // day after evidence ruled out lineHeight as the actual root cause.
  // Real root cause: enriched-markdown has hardcoded inline-code padding
  // (upstream issue #255, maintainer unresponsive as of 2026-05). The
  // chip artifact is library-specific, not RN-platform-wide. Discord /
  // Slack / Telegram / Mattermost mobile all use background + mono with
  // no visible top-heavy issue, confirming this is enriched's bug, not
  // an RN+iOS structural limitation. See:
  //   - docs/markdown-rendering-adr.md "Known limitations"
  //   - docs/markdown-renderer-research.md decision log 2026-05-19
  body: 24,
  // Heading lineHeights match each heading's fontSize × ~1.3. We MUST
  // pass these explicitly: enriched-markdown's heading defaults are
  // (h1: 36, h2: 30, h3: 26, ...) calibrated for THEIR default fontSize
  // (30/24/20/...). Mobile uses smaller heading fontSizes (20/18/16/...)
  // but if we don't override lineHeight, enriched keeps its huge defaults
  // — h1 at 20pt fontSize + 36pt lineHeight reads as wildly over-spaced.
  h1: 28,
  h2: 24,
  h3: 22,
  h4: 22,
  h5: 20,
  h6: 19,
} as const;

const MD_GAP = {
  paragraph: 12,
  headingTopLarge: 16,
  headingTopSmall: 12,
  headingBottomLarge: 8,
  headingBottomSmall: 6,
} as const;

// Inline code style — see the `code:` entry in the style object below
// for the full history (2026-05-19 transparent + brand tint workaround
// → 2026-05-19 reverted to subtle surface-2 chip + foreground text).

export function useMarkdownStyle() {
  const { isDarkColorScheme } = useColorScheme();
  const t = isDarkColorScheme ? THEME.dark : THEME.light;

  return useMemo(
    () => ({
      // Body / paragraph — text-sm + leading-6 ≈ 1.71. Generous for CJK.
      paragraph: {
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        color: t.foreground,
        marginBottom: MD_GAP.paragraph,
      },
      // Headings — Apple HIG-calibrated, one tier below shadcn web defaults.
      h1: {
        fontSize: MD_FONT.h1,
        lineHeight: MD_LINE.h1,
        fontWeight: "700" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      },
      h2: {
        fontSize: MD_FONT.h2,
        lineHeight: MD_LINE.h2,
        fontWeight: "600" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopLarge,
        marginBottom: MD_GAP.headingBottomLarge,
      },
      h3: {
        fontSize: MD_FONT.h3,
        lineHeight: MD_LINE.h3,
        fontWeight: "600" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h4: {
        fontSize: MD_FONT.h4,
        lineHeight: MD_LINE.h4,
        fontWeight: "600" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h5: {
        fontSize: MD_FONT.h5,
        lineHeight: MD_LINE.h5,
        fontWeight: "600" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      h6: {
        fontSize: MD_FONT.h6,
        lineHeight: MD_LINE.h6,
        fontWeight: "600" as const,
        color: t.foreground,
        marginTop: MD_GAP.headingTopSmall,
        marginBottom: MD_GAP.headingBottomSmall,
      },
      strong: {
        // md4c restricts inline `fontWeight` to "bold" | "normal" — it adds
        // the bold trait on top of the inherited block font. We can't pin
        // a 600 weight here the way we can on headings.
        fontWeight: "bold" as const,
        color: t.foreground,
      },
      em: {
        // STYLES.md confirms enriched adds italic by default; we set it
        // explicitly for parity with `strong` (which explicitly sets
        // fontWeight: "bold").
        fontStyle: "italic" as const,
        color: t.foreground,
      },
      strikethrough: {
        color: t.mutedForeground,
      },
      underline: {
        color: t.foreground,
      },
      link: {
        color: t.brand,
        underline: true,
      },
      // Inline code — monospace + muted-foreground tint, NO background chip.
      //
      // Why no background: on iOS, NSAttributedString draws
      // NSBackgroundColorAttributeName as a rectangle spanning the full
      // line-box height (24pt for our CJK-tuned body leading). PingFang
      // SC places the baseline at ~75-80% of the line-box, so monospaced
      // glyphs sit low inside that rectangle, leaving ~11pt empty at the
      // top vs ~6pt at the bottom — a visible 2:1 asymmetry in CJK
      // paragraphs. This is a platform-level constraint, not a library
      // bug: the library's STYLES.md exposes only fontFamily / fontSize /
      // color / backgroundColor / borderColor for inline code — no
      // padding, lineHeight, or baselineOffset knob. Confirmed 2026-05-21
      // (researcher brief): zero open PRs / forks / prereleases touching
      // inline-code vertical geometry; upstream issue #255 has no
      // maintainer response since 2026-04-20. The only real fixes are at
      // the native layer (custom NSLayoutManager.drawBackground override
      // or TextKit 2 NSTextLayoutManager.enumerateTextSegments(.highlight))
      // which would require forking the library — not worth the perpetual
      // patch-rebase cost for a polish issue.
      //
      // Visual identification stays strong without the chip: monospace
      // font + muted-foreground tint matches Apple Notes / iA Writer.
      //
      // Color choice — mutedForeground (NOT brand):
      //   - mutedForeground is a neutral gray, reads as "subdued prose".
      //   - brand is reserved for links; tinting code blue confused users
      //     into tapping it as if it were a link (tried 2026-05-19, then
      //     reverted same day for this reason).
      //
      // fontFamily intentionally NOT set: enriched-markdown's default is
      // the platform system monospace (SF Mono on iOS, monospace on
      // Android), which has a larger visual x-height than the previously-
      // explicit "Menlo" override.
      //
      // Revisit when:
      //   - upstream #255 ships a paddingVertical / lineHeight knob, OR
      //   - we contribute the TextKit 2 fix upstream (the right hook is
      //     NSTextLayoutManager.enumerateTextSegments(in:type:.highlight:))
      // Either way, switch back to a tinted-chip style for cross-platform
      // visual parity with web/desktop.
      code: {
        color: t.mutedForeground,
        backgroundColor: "transparent",
        borderColor: "transparent",
        // Match body (14) and codeBlock (14) — inline code MUST NOT be
        // larger than block code, otherwise the hierarchy inverts and
        // the inline token "jumps" out of any paragraph that contains
        // one. The earlier +1pt was meant to compensate for SF Mono's
        // smaller cap-height vs PingFang in pure-CJK paragraphs, but
        // in English-heavy text (variable names, error strings) the
        // +1 made mono glyphs visibly larger than surrounding Latin
        // body, which is the opposite of the intent. Visual id stays
        // strong via monospace family + mutedForeground tint — same
        // approach as GitHub Mobile / Linear iOS / Notion.
        fontSize: MD_FONT.body,
      },
      // Block code — bigger box, surface-2 background (one tonal tier
       // above secondary so the box stays visible when the markdown
       // renders inside a bg-secondary parent like a comment bubble),
       // mono font. (When the splitter detects a fenced code block it
       // routes to the in-house `CodeBlock` component instead — this
       // style is the fallback for any code that stays inside the
       // enriched prose stream, e.g. code nested in a list item.)
       // `borderColor` REQUIRED: enriched defaults to `#374151` which
       // clashes with our background.
      codeBlock: {
        fontSize: MD_FONT.codeBlock,
        color: t.foreground,
        backgroundColor: t.surface2,
        borderColor: t.border,
        padding: 12,
        borderRadius: 8,
        marginBottom: MD_GAP.paragraph,
      },
      // Blockquote — `color` is REQUIRED: enriched's default is a hardcoded
      // #4B5563 mid-gray that disappears on dark backgrounds.
      //
      // borderWidth: 3 (was 2) — iOS quote bars in Apple Notes / Linear /
      // Things are 3-4pt thick. 2pt was too thin to register as a visual
      // accent. Whether enriched draws this as a 3pt left bar or as a 3pt
      // full-frame border depends on the library schema (STYLES.md doesn't
      // distinguish); needs simulator verification — see TODO below.
      blockquote: {
        color: t.mutedForeground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        borderColor: t.border,
        borderWidth: 3,
        backgroundColor: "transparent",
        marginBottom: MD_GAP.paragraph,
      },
      // List — bullets in muted-foreground so they don't compete with content.
      // `color` is REQUIRED: enriched's default text color does NOT track
      // dark mode, so list items render in hardcoded near-black and are
      // invisible on dark backgrounds. This was the visible bug in #MUL-2395
      // dark-mode screenshot (2026-05-19).
      list: {
        color: t.foreground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        bulletColor: t.mutedForeground,
        bulletSize: 4,
        markerColor: t.mutedForeground,
        gapWidth: 8,
        marginLeft: 16,
      },
      image: {
        borderRadius: 8,
        marginBottom: MD_GAP.paragraph,
      },
      // Task lists. `checkedTextColor` REQUIRED: enriched default is `#000000`,
      // making completed items invisible in dark mode.
      taskList: {
        checkedColor: t.brand,
        borderColor: t.border,
        checkmarkColor: t.brandForeground,
        checkedTextColor: t.mutedForeground,
        checkboxSize: 16,
      },
      // GFM tables. Every color field below is required — enriched defaults
      // are all hardcoded light values (#FFFFFF row even, #F9FAFB row odd,
      // #111827 header text), all invisible / clashing in dark mode.
      // headerBackgroundColor uses `surface-2` (one tier above secondary)
      // so the header stays distinct when the table renders inside a
      // bg-secondary parent like a comment bubble.
      table: {
        color: t.foreground,
        fontSize: MD_FONT.body,
        lineHeight: MD_LINE.body,
        borderColor: t.border,
        // borderRadius: 8 (was 6) — aligns with codeBlock and image (both 8).
        // No reason for table to be the odd one out.
        borderRadius: 8,
        headerBackgroundColor: t.surface2,
        headerTextColor: t.foreground,
        // Transparent rows let the page background show through — works in
        // both light (white page) and dark (near-black page) without a
        // jarring inner panel.
        rowEvenBackgroundColor: "transparent",
        rowOddBackgroundColor: "transparent",
        cellPaddingHorizontal: 10,
        cellPaddingVertical: 6,
        // Was missing — every other block-level element sets marginBottom
        // (paragraph/codeBlock/blockquote/image/math all 12). Without this
        // the table sits flush against the following paragraph.
        marginBottom: MD_GAP.paragraph,
      },
      // Horizontal rule. `color` alone is not enough — without explicit
      // vertical margin the divider sits flush against neighbouring blocks
      // and reads as a sub-pixel line rather than a section separator.
      // marginTop/marginBottom = 16 matches MD_GAP.headingTopLarge so the
      // rule feels like a paragraph-level boundary.
      thematicBreak: {
        color: t.border,
        marginTop: 16,
        marginBottom: 16,
      },
      // LaTeX math (free with this engine — was V3 deferred under the walker).
      math: {
        fontSize: 16,
        color: t.foreground,
        backgroundColor: t.muted,
        padding: 12,
        marginBottom: MD_GAP.paragraph,
        textAlign: "center" as const,
      },
      inlineMath: {
        color: t.foreground,
      },
    }),
    [t],
  );
}
