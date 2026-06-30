/**
 * Unified chip row for the comment composer's pending pickables.
 *
 * Three chip kinds share the same capsule shape (icon + name + remove ×):
 *
 *   - mention  → `@<name>` (or `@all` for the workspace-wide pick). Tap is a
 *                no-op; only the × button removes. Drives the comment's
 *                mention markdown header on submit.
 *   - image    → filename + image icon. Tap opens the lightbox using the
 *                LOCAL file:// uri so completed and uploading items both
 *                preview without waiting for the server URL.
 *   - file     → filename + document icon. Tap opens the canonical
 *                download_url in Safari once the upload completed; before
 *                completion the tap is a no-op.
 *
 * Capsule (not thumbnail) by design: the previous version showed an actual
 * image preview inside a 64x64 card. The user feedback was that the preview
 * pulled the eye away from the input — the row should be a "what did I
 * attach" summary, not a visual gallery. Click-to-zoom satisfies the
 * "I want to verify it's the right image" need without competing with
 * @ and file chips for visual weight.
 *
 * Lives above the TextInput (between the reply-target chip and the input
 * itself). The composer measures whether the row is non-empty to decide
 * whether to render this component at all — empty composer keeps the
 * vertical footprint minimal.
 */
import { useMemo } from "react";
import { ActivityIndicator, Linking, Pressable, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { useLightbox } from "@/lib/markdown/lightbox-provider";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { Text } from "@/components/ui/text";

/** Mention chip data — composer-local state. No store, no cross-route
 *  sharing. The composer owns the array and passes it in. */
export type MentionChipType = "member" | "agent" | "squad" | "all" | "issue";

export interface MentionChip {
  type: MentionChipType;
  /** UUID for member/agent/squad/issue; literal "all" for @all. */
  id: string;
  /** Display name without leading `@`. For type "issue" this stores the
   *  human identifier (e.g. "MUL-123"), which is what the chip + the
   *  serialised markdown link both surface (matches web's
   *  packages/views/editor/extensions/mention-extension.ts:67-74 — issues
   *  drop the leading `@`). */
  name: string;
}

export type ComposerAttachmentStatus = "uploading" | "completed" | "failed";

export interface ComposerAttachmentItem {
  /** Stable local id assigned by the composer when the user picked. Used as
   *  the React key AND as the lookup id for status transitions. We don't use
   *  the server-returned `id` because it doesn't exist yet during upload. */
  localId: string;
  /** `file://...` from expo-image-picker / expo-document-picker. Source of
   *  truth for lightbox preview even post-upload — on-device cache. */
  localUri: string;
  filename: string;
  mimeType: string;
  status: ComposerAttachmentStatus;
  /** Populated when status === "completed" — the server-side attachment id
   *  that the comment mutation will reference via `attachmentIds`. */
  id?: string;
  /** Populated when status === "completed" — canonical `mc://file/<id>`
   *  URL the server returns. The composer submits by id, not url; the
   *  field is kept for inline-insert affordances or debugging. */
  url?: string;
  /** Populated when status === "completed" — signed HTTPS link to open in
   *  Safari for file chips. Mirrors web's "download" path. */
  downloadUrl?: string;
  /** Populated when status === "failed" — short human-readable error. */
  error?: string;
}

interface Props {
  mentions: MentionChip[];
  attachments: ComposerAttachmentItem[];
  onRemoveMention: (type: MentionChipType, id: string) => void;
  onRemoveAttachment: (localId: string) => void;
  onRetryAttachment?: (localId: string) => void;
}

export function ComposerAttachmentRow({
  mentions,
  attachments,
  onRemoveMention,
  onRemoveAttachment,
  onRetryAttachment,
}: Props) {
  if (mentions.length === 0 && attachments.length === 0) return null;

  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={{ gap: 6, paddingHorizontal: 2, paddingVertical: 2 }}
      keyboardShouldPersistTaps="handled"
    >
      {mentions.map((m) => (
        <MentionChipView
          key={`m:${m.type}:${m.id}`}
          mention={m}
          onRemove={onRemoveMention}
        />
      ))}
      {attachments.map((a) => (
        <AttachmentChipView
          key={a.localId}
          item={a}
          onRemove={onRemoveAttachment}
          onRetry={onRetryAttachment}
        />
      ))}
    </ScrollView>
  );
}

// ---------------------------------------------------------------------------
// Mention chip — small capsule, no tap (only × removes).
// ---------------------------------------------------------------------------

function MentionChipView({
  mention,
  onRemove,
}: {
  mention: MentionChip;
  onRemove: (type: MentionChipType, id: string) => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];

  // Icon picks: @all → people; issue → git-branch (matches web's status icon
  // styling for issue mentions); else single-person glyph.
  const iconName =
    mention.type === "all"
      ? "people"
      : mention.type === "issue"
        ? "git-branch-outline"
        : "person";

  // Issue chips show the bare identifier (e.g. "MUL-123") — no leading @.
  // Mirrors how the serialized markdown link renders on web/desktop.
  const label = mention.type === "issue" ? mention.name : `@${mention.name}`;

  return (
    <View className="flex-row items-center gap-1 h-7 px-2 rounded-full bg-primary/10">
      <Ionicons name={iconName} size={12} color={theme.primary} />
      <Text className="text-xs font-medium text-foreground">{label}</Text>
      <Pressable
        onPress={() => onRemove(mention.type, mention.id)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Remove mention ${mention.name}`}
        className="h-4 w-4 items-center justify-center"
      >
        <Ionicons name="close" size={12} color={theme.mutedForeground} />
      </Pressable>
    </View>
  );
}

// ---------------------------------------------------------------------------
// Attachment chip — image / file capsule with status overlay.
// ---------------------------------------------------------------------------

interface AttachmentChipProps {
  item: ComposerAttachmentItem;
  onRemove: (localId: string) => void;
  onRetry?: (localId: string) => void;
}

function AttachmentChipView({ item, onRemove, onRetry }: AttachmentChipProps) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const { open } = useLightbox();

  const isImage = useMemo(
    () => item.mimeType.startsWith("image/"),
    [item.mimeType],
  );

  const onPress = () => {
    if (item.status === "failed" && onRetry) {
      onRetry(item.localId);
      return;
    }
    if (item.status !== "completed") return;
    if (isImage) {
      // Prefer the local on-device file over the network URL — instant,
      // no signed-URL round-trip, works the same pre/post upload.
      open(item.localUri);
    } else {
      // Non-image file chip: open the canonical download URL in Safari.
      // `downloadUrl` comes from `api.uploadFile(...).download_url`, which
      // on non-CloudFront deployments is a server-relative path like
      // `/api/attachments/{id}/download` (MUL-2976). RN's `Linking.openURL`
      // requires an absolute http(s) URL — `Cannot open URL` otherwise — so
      // resolve against `EXPO_PUBLIC_API_URL` first. Already-absolute
      // CloudFront/presigned URLs pass through unchanged. `null` (no
      // downloadUrl yet) falls through to a no-op.
      const target = resolveAttachmentUrl(item.downloadUrl);
      if (target) void Linking.openURL(target);
    }
  };

  const iconName = item.status === "failed"
    ? "refresh"
    : isImage
      ? "image-outline"
      : "document-outline";

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole={item.status === "failed" ? "button" : "image"}
      accessibilityLabel={
        item.status === "failed"
          ? `Retry upload of ${item.filename}`
          : `Open ${item.filename}`
      }
      className="flex-row items-center gap-1 h-7 px-2 rounded-full bg-secondary active:opacity-80"
    >
      {item.status === "uploading" ? (
        <ActivityIndicator size="small" color={theme.mutedForeground} />
      ) : (
        <Ionicons
          name={iconName}
          size={12}
          color={
            item.status === "failed"
              ? theme.destructive
              : theme.mutedForeground
          }
        />
      )}
      <Text
        className="text-xs text-foreground max-w-[120px]"
        numberOfLines={1}
      >
        {item.filename}
      </Text>
      <Pressable
        onPress={() => onRemove(item.localId)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Remove ${item.filename}`}
        className="h-4 w-4 items-center justify-center"
      >
        <Ionicons name="close" size={12} color={theme.mutedForeground} />
      </Pressable>
    </Pressable>
  );
}
