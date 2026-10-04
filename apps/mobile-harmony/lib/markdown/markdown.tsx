/**
 * Public Markdown component — HarmonyOS port of apps/mobile/lib/markdown/
 * markdown.tsx. Same pipeline and props (content → preprocessMobileMarkdown
 * → splitMarkdown → per-segment render), with the enriched-markdown prose
 * island replaced by the in-house renderer in prose.tsx. The link-press
 * contract is identical: mention://issue|project navigates in-app via the
 * app shell's nav channel, everything else goes to the system.
 */
import { useCallback, useMemo } from "react";
import { Linking, View } from "react-native";
import type { Attachment } from "@multica/core/types";
import { useWorkspaceStore } from "@/data/workspace-store";
import { preprocessMobileMarkdown } from "@/lib/markdown/preprocess";
import { useMarkdownStyles } from "@/lib/markdown/markdown-style";
import { splitMarkdown } from "@/lib/markdown/split-markdown";
import { renderProse, type ProseLinkPress } from "@/lib/markdown/prose";
import { CodeBlock } from "@/lib/markdown/code-block";
import { MarkdownImage } from "@/lib/markdown/markdown-image";

/**
 * In-app navigation channel for mention:// links. The app shell registers
 * a dispatcher at startup (setMarkdownNavigator); markdown lives below the
 * navigator in the tree and must not import it (layering).
 */
export type MarkdownNavigate = (target: { type: string; id: string }) => void;

let markdownNavigate: MarkdownNavigate | null = null;

export function setMarkdownNavigator(nav: MarkdownNavigate | null) {
  markdownNavigate = nav;
}

interface Props {
  content: string;
  /** Attachments used to resolve `mc://file/<id>` image URIs. */
  attachments?: Attachment[];
  /** Opt out for surfaces with a competing onLongPress (comment card). */
  selectable?: boolean;
  /** Tighten vertical rhythm inside bubbles/badges (see iOS file). */
  compact?: boolean;
}

export function Markdown({
  content,
  attachments,
  selectable = true,
  compact = false,
}: Props) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const styles = useMarkdownStyles(compact);

  const segments = useMemo(() => {
    const processed = preprocessMobileMarkdown(content);
    return splitMarkdown(processed);
  }, [content]);

  const onLinkPress = useCallback<ProseLinkPress>(
    (url: string) => {
      if (url.startsWith("mention://")) {
        const rest = url.slice("mention://".length);
        const slash = rest.indexOf("/");
        if (slash < 0) return;
        const type = rest.slice(0, slash);
        const id = rest.slice(slash + 1);
        if (id && wsSlug && (type === "issue" || type === "project")) {
          markdownNavigate?.({ type, id });
        }
        return;
      }
      Linking.openURL(url).catch(() => {
        // Silent: failing loudly is worse than a no-op tap.
      });
    },
    [wsSlug],
  );

  if (segments.length === 0) return null;

  return (
    <View style={styles.segmentGap ? { gap: styles.segmentGap } : undefined}>
      {segments.map((seg, i) => {
        switch (seg.type) {
          case "prose":
            return (
              <View key={i}>
                {renderProse(seg.content, styles, onLinkPress, selectable)}
              </View>
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
