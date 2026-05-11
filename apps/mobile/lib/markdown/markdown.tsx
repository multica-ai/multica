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
import { useWorkspaceStore } from "@/data/workspace-store";
import { preprocessMobileMarkdown } from "./preprocess";
import { MARKDOWN_STYLE } from "./markdown-style";
import { splitMarkdown } from "./split-markdown";
import { CodeBlock } from "./code-block";
import { MarkdownImage } from "./markdown-image";

interface Props {
  content: string;
}

export function Markdown({ content }: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const segments = useMemo(() => {
    const processed = preprocessMobileMarkdown(content);
    return splitMarkdown(processed);
  }, [content]);

  const onLinkPress = useCallback(
    ({ url }: { url: string }) => {
      // mention://issue/<uuid> → navigate to issue detail. Other mention
      // types (member / agent) currently no-op; future iteration could
      // open a profile sheet.
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
                markdownStyle={MARKDOWN_STYLE}
                onLinkPress={onLinkPress}
              />
            );
          case "code":
            return <CodeBlock key={i} code={seg.code} lang={seg.lang} />;
          case "image":
            return <MarkdownImage key={i} uri={seg.uri} alt={seg.alt} />;
        }
      })}
    </View>
  );
}
