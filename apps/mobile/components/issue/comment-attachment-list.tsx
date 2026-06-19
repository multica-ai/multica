/**
 * Standalone attachment list for comment cards.
 *
 * Mirrors the design of web's `AttachmentList` in
 * `packages/views/issues/components/comment-card.tsx:121-159` — renders
 * any attachment whose URL the markdown content didn't already reference,
 * with same-file dedup so a duplicate upload referenced inline doesn't
 * also appear below.
 *
 * The data-contract parity goal: a comment authored on mobile (which has
 * no inline-insert path — see `inline-comment-composer.tsx`) carries its
 * attachments via the `attachments` field only, with no `![](url)` in
 * `content`. Web reads it back and `AttachmentList` puts the attachments
 * below the body. Mobile reads it back here and does the same. A comment
 * authored on web with inline images already inside the markdown renders
 * inline on both clients via `MarkdownImage`, and this list returns null
 * because there's nothing "leftover" to show.
 *
 * For v1 we render images via the same `MarkdownImage` used by inline
 * markdown rendering (consistent aspect-ratio + lightbox behavior). Non-
 * image attachments render as a tappable file card showing 📎 + filename
 * + size hint, opening the canonical download URL on tap.
 */
import { useMemo } from "react";
import { Linking, Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { Attachment } from "@multica/core/types";
import { MarkdownImage } from "@/lib/markdown/markdown-image";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { Text } from "@/components/ui/text";

interface Props {
  attachments?: Attachment[];
  /** The comment's markdown content. Attachments referenced inside it via
   *  `![](url)` or `[name](url)` are skipped so they aren't double-rendered.
   *  Pass `undefined` (not just an empty string) when the comment has no
   *  body — that disables the inline-reference filter and renders all
   *  supplied attachments. */
  content?: string;
}

export function CommentAttachmentList({ attachments, content }: Props) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  const standalone = useMemo(() => {
    if (!attachments || attachments.length === 0) return [];
    if (!content) return attachments;
    return attachments.filter((a) => {
      // Skip attachments whose URL is already referenced inline in the
      // markdown — they'll render via MarkdownImage (images) or a markdown
      // link (files), and we'd otherwise show them twice.
      if (content.includes(a.url)) return false;
      // Dedup: if another attachment with the same file identity (name,
      // type, size) is already inline in the content, this one is a
      // duplicate upload — skip it. Mirrors web's
      // `comment-card.tsx:132-140` defense.
      const hasSiblingInContent = attachments.some(
        (other) =>
          other.id !== a.id &&
          other.filename === a.filename &&
          other.content_type === a.content_type &&
          other.size_bytes === a.size_bytes &&
          content.includes(other.url),
      );
      if (hasSiblingInContent) return false;
      return true;
    });
  }, [attachments, content]);

  if (standalone.length === 0) return null;

  return (
    <View className="gap-1.5">
      {standalone.map((attachment) => {
        const isImage = attachment.content_type.startsWith("image/");
        if (isImage) {
          return (
            <MarkdownImage
              key={attachment.id}
              uri={attachment.url}
              alt={attachment.filename}
              attachments={attachments}
            />
          );
        }
        return (
          <FileCard
            key={attachment.id}
            attachment={attachment}
            theme={theme}
          />
        );
      })}
    </View>
  );
}

function FileCard({
  attachment,
  theme,
}: {
  attachment: Attachment;
  theme: typeof THEME["light"];
}) {
  const sizeLabel = formatBytes(attachment.size_bytes);
  return (
    <Pressable
      onPress={() => {
        // download_url is the canonical link — opening it hands off to
        // Safari which handles auth-token-free download + previewing for
        // common types (PDF, txt). Mirrors what the markdown link renderer
        // does for `[name](url)`.
        //
        // The backend may return a server-relative URL like
        // `/api/attachments/{id}/download` when no CloudFront signer is
        // configured (MUL-2976). RN's `Linking.openURL` requires an
        // absolute http(s) URL — it returns "Cannot open URL" otherwise —
        // so resolve against `EXPO_PUBLIC_API_URL` first.
        const target = resolveAttachmentUrl(attachment.download_url);
        if (target) {
          void Linking.openURL(target);
        }
      }}
      accessibilityRole="button"
      accessibilityLabel={`Open ${attachment.filename}`}
      className="flex-row items-center gap-2 px-3 py-2 rounded-md bg-secondary/60 active:opacity-80"
    >
      <Ionicons
        name="document-outline"
        size={20}
        color={theme.mutedForeground}
      />
      <View className="flex-1">
        <Text
          className="text-sm text-foreground"
          numberOfLines={1}
        >
          {attachment.filename}
        </Text>
        {sizeLabel ? (
          <Text className="text-xs text-muted-foreground">{sizeLabel}</Text>
        ) : null}
      </View>
      <Ionicons
        name="download-outline"
        size={18}
        color={theme.mutedForeground}
      />
    </Pressable>
  );
}

function formatBytes(bytes: number): string | null {
  if (!bytes || bytes <= 0) return null;
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex++;
  }
  const formatted =
    value < 10 ? value.toFixed(1) : Math.round(value).toString();
  return `${formatted} ${units[unitIndex]}`;
}
