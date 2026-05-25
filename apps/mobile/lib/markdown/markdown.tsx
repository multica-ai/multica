/**
 * Public Markdown component for the mobile app. Hybrid renderer:
 *
 *   - Prose (paragraphs, headings, lists, quotes, tables, inline code,
 *     links, mentions) → `EnrichedMarkdownText` (react-native-enriched-
 *     markdown, native md4c → NSAttributedString / Spannable). One
 *     instance per prose island.
 *   - Fenced code blocks → in-house `CodeBlock` with Shiki syntax
 *     highlighting, copy button, and horizontal scroll. Shares the
 *     `github-light` / `github-dark` themes with web for byte-identical
 *     palettes.
 *   - Images → in-house `MarkdownImage` with expo-image + auto aspect
 *     ratio + tap-to-lightbox dispatch.
 *
 * Why hybrid instead of pure enriched: enriched does not let us inject
 * React for any leaf node (issues #54, #232 — maintainer: "no custom
 * renderers, by design"), which would permanently lock out syntax
 * highlighting and tap-to-lightbox. The maintainer themselves
 * recommends this split in #246: "split them out and render with
 * another instance of enriched-markdown".
 *
 * Pipeline:
 *
 *   content
 *     ↓ preprocessMobileMarkdown    legacy mention shortcodes + file
 *                                   cards + HTML strip with `<br>` →
 *                                   "  \n" (canonical CommonMark hard
 *                                   break)
 *     ↓ splitMarkdown               marked.lexer → segments[]
 *     ↓ render per-segment          prose / code / image components
 *
 * Mention chip note: mobile renders `mention://` links via enriched's
 * default link styling (brand-colored, underlined), matching web's
 * fallback behavior when no `renderMention` is provided
 * (`packages/ui/markdown/Markdown.tsx:173-178`). The avatar pill
 * variant only ever existed on web in specific contexts that supplied
 * a custom renderer — mobile doesn't lose anything that exists by
 * default elsewhere.
 */
import { useCallback, useMemo } from "react";
import { Linking, View } from "react-native";
import { router } from "expo-router";
import { EnrichedMarkdownText } from "react-native-enriched-markdown";
import type { Attachment } from "@multica/core/types";
import { useWorkspaceStore } from "@/data/workspace-store";
import { preprocessMobileMarkdown } from "./preprocess";
import { useMarkdownStyle } from "./markdown-style";
import { splitMarkdown } from "./split-markdown";
import { CodeBlock } from "./code-block";
import { MarkdownImage } from "./markdown-image";

interface Props {
  content: string;
  /**
   * Attachments scoped to the same record this content belongs to (issue,
   * comment's parent issue, chat message). Used to resolve `mc://file/<id>`
   * image URIs to a real HTTPS `download_url` — without it, iOS image loader
   * doesn't understand the mc: scheme and the image fails to load.
   */
  attachments?: Attachment[];
  /**
   * When `false`, kills the UIKit-native long-press selection (the magnifier
   * + selection handles) inside both the enriched prose and the in-house
   * `<CodeBlock>` content. Required for surfaces that own a competing
   * `onLongPress` gesture — most notably the comment card, whose long-press
   * opens the comment action sheet. Without `selectable={false}` the OS
   * gesture and the Pressable both fire and the selection magnifier
   * appears on top of the action sheet (Element X PR #1584 documents the
   * identical bug in a Matrix client).
   *
   * Default is `true` for reader surfaces (issue description, chat message
   * bodies) where users expect to be able to select and copy text natively.
   */
  selectable?: boolean;
  /**
   * Strip the trailing paragraph `marginBottom` AND tighten paragraph
   * `lineHeight` so the markdown sits flush AND visually centered inside
   * a bounded container (chat user bubble, badge, tooltip — anything that
   * already supplies its own vertical padding).
   *
   * Two adjustments because two separate offsets break vertical centering
   * in a small box:
   *
   *   1. `marginBottom: 0` — mirrors web's `[&>*:last-child]:mb-0`
   *      neutralisation pattern in
   *      `packages/views/chat/components/chat-message-list.tsx`.
   *
   *   2. `lineHeight: 20` (down from 24) — paragraph default is calibrated
   *      for prose readability (`lineHeight/fontSize ≈ 1.71` for CJK
   *      leading), which makes the line-box 10pt taller than the glyph.
   *      Combined with PingFang SC's baseline at ~75-80% of the line-box
   *      (see `markdown-style.ts:54-67` and the inline-code note at 184-200),
   *      the empty space inside the line-box is asymmetric — visible as
   *      the glyph sitting off-centre in a small `py-2` bubble. Tightening
   *      to ~1.43 collapses the line-box close to the glyph and the
   *      residual asymmetry becomes too small to read as misalignment.
   *      Multi-line content in compact mode loses some inter-line breathing
   *      room — acceptable for the common single-paragraph bubble case.
   *
   * enriched-markdown applies one paragraph style to every paragraph
   * (can't single out the last), so multi-paragraph content in compact mode
   * also loses the 12px inter-paragraph gap — a fair trade for the bubble
   * case.
   */
  compact?: boolean;
}

export function Markdown({
  content,
  attachments,
  selectable = true,
  compact = false,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const baseStyle = useMarkdownStyle();
  const markdownStyle = useMemo(
    () =>
      compact
        ? {
            ...baseStyle,
            paragraph: {
              ...baseStyle.paragraph,
              marginBottom: 0,
              lineHeight: 20,
            },
          }
        : baseStyle,
    [baseStyle, compact],
  );

  const segments = useMemo(() => {
    const processed = preprocessMobileMarkdown(content);
    return splitMarkdown(processed);
  }, [content]);

  const onLinkPress = useCallback(
    ({ url }: { url: string }) => {
      // `mention://` is an internal scheme — never hand it to the system.
      // No app is registered for it, so `Linking.openURL("mention://...")`
      // would surface iOS's "Cannot open URL" prompt or silently fail
      // (depending on iOS version). Handle every shape inline and ALWAYS
      // return without falling through to Linking.
      //
      //   mention://issue/<uuid>   → navigate to that issue detail
      //   mention://member/<uuid>  → no-op (no member profile screen yet)
      //   mention://agent/<uuid>   → no-op (no agent profile screen yet)
      //   mention://squad/<uuid>   → no-op (no squad profile screen yet)
      //   mention://all/all        → no-op (semantic only — "everyone")
      //   anything malformed       → no-op
      if (url.startsWith("mention://")) {
        const rest = url.slice("mention://".length);
        const slash = rest.indexOf("/");
        if (slash < 0) return;
        const type = rest.slice(0, slash);
        const id = rest.slice(slash + 1);
        if (type === "issue" && id && wsSlug) {
          router.push(`/${wsSlug}/issue/${id}`);
        }
        return;
      }
      // Everything else — http(s), mailto, tel, app-scheme deep links —
      // hand off to the system. Linking.openURL throws if no app handles
      // the URL; the catch keeps a stray tap from crashing the screen.
      Linking.openURL(url).catch(() => {
        // Silent: failing loudly is worse than a no-op tap.
      });
    },
    [wsSlug],
  );

  if (segments.length === 0) return null;

  return (
    // `gap-3` enforces 12px between every segment sibling (prose ↔ code,
    // code ↔ code, image ↔ anything). Going through gap instead of
    // per-child marginVertical avoids a NativeWind 4 / Yoga quirk where
    // adjacent `marginVertical` siblings collapse closer than the sum
    // would suggest — `gap` is layout-level spacing that doesn't depend
    // on margin behaviour.
    <View className="gap-3">
      {segments.map((seg, i) => {
        switch (seg.type) {
          case "prose":
            return (
              <EnrichedMarkdownText
                key={i}
                flavor="github"
                markdown={seg.content}
                markdownStyle={markdownStyle}
                onLinkPress={onLinkPress}
                selectable={selectable}
              />
            );
          case "code":
            return (
              <CodeBlock
                key={i}
                code={seg.code}
                lang={seg.lang}
                selectable={selectable}
              />
            );
          case "image":
            return (
              <MarkdownImage
                key={i}
                uri={seg.uri}
                alt={seg.alt}
                attachments={attachments}
              />
            );
        }
      })}
    </View>
  );
}
