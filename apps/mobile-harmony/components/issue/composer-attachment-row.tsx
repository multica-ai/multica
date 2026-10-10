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
 *                download_url once the upload completed; before
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
 *
 * HarmonyOS port: StyleSheet styles instead of NativeWind classes, and the
 * iOS shared LightboxProvider is replaced with a per-image RN <Modal> — the
 * same substitution lib/markdown/markdown-image.tsx makes on this platform
 * (no image-sequence swipe; roadmap work).
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, Image, Linking, Modal, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { Icon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

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
   *  packages/views/editor/extensions/mention-extension.ts — issues
   *  drop the leading `@`). */
  name: string;
}

export type ComposerAttachmentStatus = "uploading" | "completed" | "failed";

export interface ComposerAttachmentItem {
  /** Stable local id assigned by the composer when the user picked. Used as
   *  the React key AND as the lookup id for status transitions. We don't use
   *  the server-returned `id` because it doesn't exist yet during upload. */
  localId: string;
  /** `file://...` from the media picker. Source of truth for the lightbox
   *  preview even post-upload — on-device cache. */
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
  /** Populated when status === "completed" — signed HTTPS link to open for
   *  file chips. Mirrors web's "download" path. */
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
      contentContainerStyle={styles.rowContent}
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

const styles = StyleSheet.create({
  // gap-6px paddingHorizontal-2 paddingVertical-2
  rowContent: { gap: 6, paddingHorizontal: 2, paddingVertical: 2 },
  // flex-row items-center gap-1 h-7 px-2 rounded-full
  capsule: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    height: 28,
    paddingHorizontal: 8,
    borderRadius: 999,
  },
  // active:opacity-80
  capsulePressed: { opacity: 0.8 },
  // text-xs font-medium
  chipLabel: { fontSize: 12, fontWeight: "500" },
  // h-4 w-4 items-center justify-center
  removeButton: {
    height: 16,
    width: 16,
    alignItems: "center",
    justifyContent: "center",
  },
  // text-xs max-w-[120px]
  attachmentName: { fontSize: 12, maxWidth: 120 },
  lightbox: {
    flex: 1,
    backgroundColor: "rgba(0,0,0,0.92)",
    alignItems: "center",
    justifyContent: "center",
  },
  lightboxImage: { width: "100%", height: "100%" },
});

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
  const c = useThemeColors();

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
    <View
      style={[styles.capsule, { backgroundColor: withAlpha(c.primary, 0.1) }]}
    >
      <Icon name={iconName} size={12} color={c.primary} />
      <Text style={[styles.chipLabel, { color: c.foreground }]}>{label}</Text>
      <Pressable
        onPress={() => onRemove(mention.type, mention.id)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Remove mention ${mention.name}`}
        style={styles.removeButton}
      >
        <Icon name="close" size={12} color={c.mutedForeground} />
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
  const c = useThemeColors();
  const [lightboxUri, setLightboxUri] = useState<string | null>(null);

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
      setLightboxUri(item.localUri);
    } else {
      // Non-image file chip: open the canonical download URL externally.
      // `downloadUrl` comes from `api.uploadFile(...).download_url`, which
      // on non-CDN deployments is a server-relative path like
      // `/api/attachments/{id}/download` (MUL-2976). RN's `Linking.openURL`
      // requires an absolute http(s) URL — `Cannot open URL` otherwise — so
      // resolve against `MULTICA_API_URL` first. Already-absolute
      // presigned URLs pass through unchanged. `null` (no downloadUrl yet)
      // falls through to a no-op.
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
    <>
      <Pressable
        onPress={onPress}
        accessibilityRole={item.status === "failed" ? "button" : "image"}
        accessibilityLabel={
          item.status === "failed"
            ? `Retry upload of ${item.filename}`
            : `Open ${item.filename}`
        }
        style={({ pressed }) => [
          styles.capsule,
          { backgroundColor: c.secondary },
          pressed && styles.capsulePressed,
        ]}
      >
        {item.status === "uploading" ? (
          <ActivityIndicator size="small" color={c.mutedForeground} />
        ) : (
          <Icon
            name={iconName}
            size={12}
            color={item.status === "failed" ? c.destructive : c.mutedForeground}
          />
        )}
        <Text
          style={[styles.attachmentName, { color: c.foreground }]}
          numberOfLines={1}
        >
          {item.filename}
        </Text>
        <Pressable
          onPress={() => onRemove(item.localId)}
          hitSlop={8}
          accessibilityRole="button"
          accessibilityLabel={`Remove ${item.filename}`}
          style={styles.removeButton}
        >
          <Icon name="close" size={12} color={c.mutedForeground} />
        </Pressable>
      </Pressable>
      {lightboxUri ? (
        <Modal
          visible
          transparent
          animationType="fade"
          onRequestClose={() => setLightboxUri(null)}
        >
          <Pressable
            style={styles.lightbox}
            onPress={() => setLightboxUri(null)}
            accessibilityLabel={`Close ${item.filename} preview`}
          >
            <Image
              source={{ uri: lightboxUri }}
              style={styles.lightboxImage}
              resizeMode="contain"
            />
          </Pressable>
        </Modal>
      ) : null}
    </>
  );
}
