/**
 * Shared message composer used by both the issue-comment thread and the
 * chat tab. Two visual states:
 *
 *   collapsed → pill button (configurable label / icon). Minimal vertical
 *               footprint so the list above gets the full screen.
 *   expanded  → optional reply chip → chip row (@ + image + file) →
 *               text input → toolbar (`@ 📷 📎 ──── [➤ or Stop]`).
 *
 * Mentions / images / files all live in the chip row OUTSIDE the text
 * input. The input itself is a plain multiline text field — no controlled
 * selection, no inline overlays. On submit the composer prepends mention
 * markdown links to the typed text and attaches `attachmentIds`.
 * Server-side mention regex (`server/internal/util/mention.go:16`) parses
 * them as if they were inline.
 *
 * Mention picker is a `MentionPickerSheet` (BottomSheet — this platform's
 * stand-in for the iOS formSheet route). The sheet writes selections into
 * `useMentionDraftStore`; this composer reads from the same store, so the
 * data flow is identical to the iOS route-push version.
 *
 * Why a shared component:
 *   - Comment and chat composers want byte-identical UI / interaction.
 *   - Chat-specific differences are slim: controlled draft text (parent
 *     owns the value for cross-session persistence), Stop button during
 *     agent execution. Both addressed via optional props.
 *
 * What this component does NOT own:
 *   - The submit action — `onSubmit` is the caller's escape hatch (it
 *     wires `useCreateComment` on the comment side, the chat send burst
 *     on the chat side).
 *   - Reply target lifecycle — comment passes in `replyTarget` +
 *     `onClearReplyTarget` from its store; chat doesn't.
 *   - Stop visual / animation — chat passes a `renderStop()` slot when
 *     `isSending` is true.
 *
 * Cleanup: mention draft store cleared on unmount so navigating away
 * from comment-A's draft doesn't leak `@张三` into comment-B's composer.
 */
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { Alert, Keyboard, StyleSheet, TextInput, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { api } from "@/data/api";
import { useMentionDraftStore } from "@/data/stores/mention-draft-store";
import * as Haptics from "@/lib/haptics";
import { filenameFromUri, mimeTypeFromFilename, pickImage, pickDocument } from "@/lib/media-picker";
import { stripMarkdown } from "@/lib/strip-markdown";
import { withAlpha } from "@/lib/theme";
import { useSafeAreaInsets } from "@/lib/safe-area";
import { useThemeColors } from "@/lib/use-theme-colors";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { Icon } from "@/components/ui/icon";
import { IconButton } from "@/components/ui/icon-button";
import { Text } from "@/components/ui/text";
import {
  ComposerAttachmentRow,
  type ComposerAttachmentItem,
  type MentionChip,
} from "@/components/issue/composer-attachment-row";
import { MentionPickerSheet } from "@/components/issue/pickers/mention";

export interface MessageComposerReplyTarget {
  actorName: string;
  preview: string;
}

interface Props {
  /** Submit callback. Composer awaits this; on rejection it restores text,
   *  attachments, and mentions so the user can retry without losing
   *  context. Resolved promise → text + chips cleared, composer collapses
   *  back to pill. */
  onSubmit: (args: {
    content: string;
    attachmentIds: string[];
    mentions: MentionChip[];
  }) => Promise<void>;

  /** Sections the mention sheet offers. "comment" (default) = @all +
   *  people + agents + squads + issues; "chat" = issues only. iOS pushed
   *  a formSheet route with a `?mode=` param; here the composer owns the
   *  sheet directly. */
  mentionPickerMode?: "comment" | "chat";

  /** Attachment upload context — forwarded to `api.uploadFile`. Comment
   *  passes `issueId`; chat omits both (uploads are session-scoped via
   *  the message id assigned by the server post-send). */
  uploadContext?: { issueId?: string; commentId?: string };

  placeholder?: string;
  pillLabel?: string;
  pillIcon?: ComponentProps<typeof Icon>["name"];

  /** Optional controlled-text mode. When `value` + `onChangeText` are
   *  both provided, the parent owns the draft (chat: persists to its
   *  draft store across sessions). When omitted, composer manages its
   *  own internal text state (comment). */
  value?: string;
  onChangeText?: (next: string) => void;

  /** Optional reply chip (comment only). */
  replyTarget?: MessageComposerReplyTarget | null;
  onClearReplyTarget?: () => void;

  /** Composer enters "auto-expanded + focused" mode when this changes to
   *  a truthy stable key. Comment uses it to react to long-press → reply
   *  flow. Chat doesn't pass it. */
  expandTrigger?: string | null;

  /** When `isSending` is true AND `renderStop` is provided, the trailing
   *  send button is replaced by whatever `renderStop` returns. Chat uses
   *  this to show a Stop affordance while the agent is running. */
  isSending?: boolean;
  renderStop?: () => ReactNode;

  /** Hard-disable. Used when chat has no usable agent. The pill shows
   *  `disabledReason` instead of `pillLabel`, and the pill is
   *  non-interactive (cannot expand). */
  disabled?: boolean;
  disabledReason?: string;

  /** When true the composer renders flush at the bottom of its parent
   *  WITHOUT the keyboard-aware lift + safe-area inset. Chat's parent
   *  owns its own KeyboardAvoidingView and bottom-inset handling (chat
   *  screen), so the composer must not also apply them. Comment's parent
   *  does NOT handle keyboard, so the composer keeps the default `true`. */
  manageKeyboard?: boolean;
}

function makeLocalId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

/** Serialises mention chips into the markdown link form the backend
 *  regex parser recognises. The string lands at the START of the
 *  outgoing content; mobile can't position mentions inline because the
 *  TextInput is plain. Acceptable semantic difference vs web/desktop's
 *  rich editor (web supports anywhere-in-text). */
function serializeMentions(chips: MentionChip[]): string {
  return chips
    .map((m) => {
      const label =
        m.type === "issue"
          ? m.name
          : m.type === "all"
            ? "@all"
            : `@${m.name}`;
      return `[${label}](mention://${m.type}/${m.id})`;
    })
    .join(" ");
}

export function MessageComposer({
  onSubmit,
  mentionPickerMode = "comment",
  uploadContext,
  placeholder = "Type a message…",
  pillLabel = "Type a message…",
  pillIcon = "chatbubble-outline",
  value: controlledValue,
  onChangeText: controlledOnChange,
  replyTarget = null,
  onClearReplyTarget,
  expandTrigger,
  isSending = false,
  renderStop,
  disabled = false,
  disabledReason,
  manageKeyboard = true,
}: Props) {
  const c = useThemeColors();
  const keyboardHeight = useKeyboardHeight();
  const insets = useSafeAreaInsets();
  const inputRef = useRef<TextInput>(null);
  const [expanded, setExpanded] = useState(false);
  const [mentionSheetVisible, setMentionSheetVisible] = useState(false);
  const [internalText, setInternalText] = useState("");
  const [attachments, setAttachments] = useState<ComposerAttachmentItem[]>([]);
  const [submitting, setSubmitting] = useState(false);

  // Hybrid controlled / uncontrolled pattern (React-canonical). Chat
  // passes `value`/`onChangeText` for cross-session draft persistence;
  // comment omits both and the composer manages local state.
  const isControlled =
    controlledValue !== undefined && controlledOnChange !== undefined;
  const text = isControlled ? controlledValue : internalText;
  const setText = useCallback(
    (next: string) => {
      if (isControlled) {
        controlledOnChange(next);
      } else {
        setInternalText(next);
      }
    },
    [isControlled, controlledOnChange],
  );

  const mentions = useMentionDraftStore((s) => s.mentions);
  const removeMention = useMentionDraftStore((s) => s.remove);
  const clearMentions = useMentionDraftStore((s) => s.clear);

  // Drop mention draft on composer unmount so navigating away doesn't
  // leak chips into the next composer's session.
  useEffect(() => {
    return () => {
      clearMentions();
    };
  }, [clearMentions]);

  // Auto-expand + focus when an `expandTrigger` changes. Comment uses
  // this to react to the long-press → reply flow setting a reply target.
  const triggerSeen = useRef<string | null>(null);
  if (
    expandTrigger &&
    triggerSeen.current !== expandTrigger &&
    !disabled
  ) {
    triggerSeen.current = expandTrigger;
    setExpanded(true);
    requestAnimationFrame(() => inputRef.current?.focus());
  }

  const hasInFlightUpload = attachments.some((a) => a.status === "uploading");
  const canSend =
    !disabled &&
    !isSending &&
    !submitting &&
    !hasInFlightUpload &&
    (text.trim().length > 0 || mentions.length > 0);

  const expand = useCallback(() => {
    if (disabled) return;
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    setExpanded(true);
    // Tapping the pill = "I want to write a new message". Drop any
    // lingering reply target so a stale chip from a prior long-press →
    // dismiss-without-send cycle doesn't bleed into the fresh draft.
    onClearReplyTarget?.();
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [disabled, onClearReplyTarget]);

  const handleSubmit = useCallback(async () => {
    if (!canSend) return;
    const textSnap = text;
    const mentionsSnap = mentions;
    const attachmentsSnap = attachments;

    const mentionMd = serializeMentions(mentionsSnap);
    const trimmed = textSnap.trim();
    const content = mentionMd
      ? trimmed
        ? `${mentionMd} ${trimmed}`
        : mentionMd
      : trimmed;

    const activeIds = attachmentsSnap
      .filter((a) => a.status === "completed")
      .map((a) => a.id)
      .filter((id): id is string => !!id);

    // Optimistic clear: text + chips empty out immediately so the next
    // typing tick doesn't double-include them. Restored on rejection.
    setText("");
    setAttachments([]);
    clearMentions();
    setSubmitting(true);
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});

    try {
      await onSubmit({
        content,
        attachmentIds: activeIds,
        mentions: mentionsSnap,
      });
      // Success → fully exit composing mode. Explicit triple-step
      // because a missing blur leaves the keyboard up; missing
      // Keyboard.dismiss races when focus is in-flight; missing
      // setExpanded(false) leaves the expanded card on screen.
      inputRef.current?.blur();
      Keyboard.dismiss();
      setExpanded(false);
    } catch {
      setText(textSnap);
      setAttachments(attachmentsSnap);
      mentionsSnap.forEach((m) =>
        useMentionDraftStore.getState().toggle(m),
      );
    } finally {
      setSubmitting(false);
    }
  }, [
    canSend,
    text,
    mentions,
    attachments,
    setText,
    clearMentions,
    onSubmit,
  ]);

  /** Streams a picked asset to /api/upload-file, updating the matching
   *  thumbnail's status as it goes. Pulled out so retry can call it
   *  again without re-opening the picker. */
  const startUpload = useCallback(
    async (
      localId: string,
      asset: { uri: string; name: string; type: string },
    ) => {
      try {
        const result = await api.uploadFile(asset, uploadContext);
        setAttachments((prev) =>
          prev.map((it) =>
            it.localId === localId
              ? {
                  ...it,
                  status: "completed",
                  id: result.id,
                  url: result.url,
                  downloadUrl: result.download_url,
                }
              : it,
          ),
        );
      } catch (err) {
        const message = err instanceof Error ? err.message : "Unknown error";
        setAttachments((prev) =>
          prev.map((it) =>
            it.localId === localId
              ? { ...it, status: "failed", error: message }
              : it,
          ),
        );
      }
    },
    [uploadContext],
  );

  const onImagePress = useCallback(async () => {
    // The picker seam resolves a cache `file://` URI, or null on cancel /
    // unavailable module. The byte size isn't surfaced on this platform,
    // so the pre-upload MAX_FILE_SIZE alert is skipped — the server's
    // own limit applies and a rejection lands in the failed-chip state.
    const uri = await pickImage();
    if (!uri) return;
    const filename = filenameFromUri(uri);
    const mimeType = mimeTypeFromFilename(filename, "image/jpeg");
    const localId = makeLocalId();
    setAttachments((prev) => [
      ...prev,
      {
        localId,
        localUri: uri,
        filename,
        mimeType,
        status: "uploading",
      },
    ]);
    requestAnimationFrame(() => inputRef.current?.focus());
    await startUpload(localId, { uri, name: filename, type: mimeType });
  }, [startUpload]);

  const onFilePress = useCallback(async () => {
    const uri = await pickDocument();
    if (!uri) return;
    const filename = filenameFromUri(uri);
    const mimeType = mimeTypeFromFilename(filename, "application/octet-stream");
    const localId = makeLocalId();
    setAttachments((prev) => [
      ...prev,
      {
        localId,
        localUri: uri,
        filename,
        mimeType,
        status: "uploading",
      },
    ]);
    requestAnimationFrame(() => inputRef.current?.focus());
    await startUpload(localId, { uri, name: filename, type: mimeType });
  }, [startUpload]);

  const onRemoveAttachment = useCallback((localId: string) => {
    setAttachments((prev) => prev.filter((it) => it.localId !== localId));
  }, []);

  const onRetryAttachment = useCallback(
    (localId: string) => {
      const item = attachments.find((it) => it.localId === localId);
      if (!item) return;
      setAttachments((prev) =>
        prev.map((it) =>
          it.localId === localId
            ? { ...it, status: "uploading", error: undefined }
            : it,
        ),
      );
      void startUpload(localId, {
        uri: item.localUri,
        name: item.filename,
        type: item.mimeType,
      });
    },
    [attachments, startUpload],
  );

  const onAtPress = useCallback(() => {
    Haptics.selectionAsync().catch(() => {});
    setMentionSheetVisible(true);
  }, []);

  /** Auto-collapse to pill when input loses focus AND nothing's worth
   *  keeping the composer expanded for. Deferred one tick so a toolbar
   *  IconButton tap (which briefly resigns first responder) doesn't
   *  trigger a collapse before its onPress runs. */
  const onBlur = useCallback(() => {
    setTimeout(() => {
      const empty =
        text.trim().length === 0 &&
        attachments.length === 0 &&
        mentions.length === 0;
      if (empty && !inputRef.current?.isFocused()) {
        setExpanded(false);
        onClearReplyTarget?.();
      }
    }, 80);
  }, [text, attachments.length, mentions.length, onClearReplyTarget]);

  const bottomInset = (manageKeyboard ? insets.bottom : 0);

  const pillContent = (
    <View style={[styles.pillWrap, { backgroundColor: c.background, borderTopColor: c.border, paddingBottom: bottomInset + 8 }]}>
      <Pressable
        onPress={expand}
        disabled={disabled}
        accessibilityRole="button"
        accessibilityLabel={disabled && disabledReason ? disabledReason : pillLabel}
        accessibilityState={{ disabled }}
        style={({ pressed }) => [
          styles.pill,
          { backgroundColor: c.secondary },
          pressed && styles.pressed,
        ]}
      >
        <Icon name={pillIcon} size={18} color={c.mutedForeground} />
        <Text style={[styles.pillLabel, { color: c.mutedForeground }]}>
          {disabled && disabledReason ? disabledReason : pillLabel}
        </Text>
      </Pressable>
    </View>
  );

  const expandedContent = (
    <View
      style={[
        styles.expandedWrap,
        { backgroundColor: c.background, paddingBottom: bottomInset + 4 },
      ]}
    >
      {replyTarget && (
        <View
          style={[
            styles.replyChip,
            { backgroundColor: withAlpha(c.secondary, 0.6) },
          ]}
        >
          <View style={styles.replyRow}>
            <Icon name="return-up-back" size={14} color={c.mutedForeground} />
            <Text
              style={[styles.replyName, { color: c.mutedForeground }]}
              numberOfLines={1}
            >
              Replying to {replyTarget.actorName}
            </Text>
            <Pressable
              onPress={onClearReplyTarget}
              hitSlop={8}
              accessibilityRole="button"
              accessibilityLabel="Cancel reply"
              style={styles.replyCancel}
            >
              <Icon name="close-circle" size={16} color={c.mutedForeground} />
            </Pressable>
          </View>
          {replyTarget.preview ? (
            <Text
              style={[styles.replyPreview, { color: c.mutedForeground }]}
              numberOfLines={2}
            >
              {stripMarkdown(replyTarget.preview)}
            </Text>
          ) : null}
        </View>
      )}

      <View
        style={[
          styles.card,
          { borderColor: c.border, backgroundColor: c.secondary, borderCurve: "continuous" },
        ]}
      >
        {mentions.length > 0 || attachments.length > 0 ? (
          <View style={styles.chipRowWrap}>
            <ComposerAttachmentRow
              mentions={mentions}
              attachments={attachments}
              onRemoveMention={removeMention}
              onRemoveAttachment={onRemoveAttachment}
              onRetryAttachment={onRetryAttachment}
            />
          </View>
        ) : null}

        <AutosizeTextArea
          ref={inputRef}
          value={text}
          onChangeText={setText}
          onBlur={onBlur}
          placeholder={placeholder}
          placeholderTextColor={c.mutedForeground}
          editable={!disabled}
          minHeight={28}
          maxHeight={140}
          style={styles.input}
        />

        <View style={styles.toolbarRow}>
          {/* @ leads the toolbar — highest-signal attachment (only one
           *  that drives notifications) and cross-resource (people +
           *  issues), pride-of-place left. */}
          <IconButton
            name="at"
            iconSize={20}
            color={mentions.length > 0 ? c.primary : undefined}
            onPress={onAtPress}
            accessibilityLabel="Mention someone or an issue"
            style={styles.toolbarButton}
          />
          <IconButton
            name="image-outline"
            iconSize={20}
            onPress={onImagePress}
            accessibilityLabel="Upload image"
            style={styles.toolbarButton}
          />
          <IconButton
            name="attach-outline"
            iconSize={20}
            onPress={onFilePress}
            accessibilityLabel="Upload file"
            style={styles.toolbarButton}
          />
          <View style={styles.toolbarSpacer} />
          {isSending && renderStop ? (
            renderStop()
          ) : (
            <IconButton
              name="arrow-up"
              iconSize={18}
              color={c.primaryForeground}
              variant="default"
              onPress={handleSubmit}
              disabled={!canSend}
              hitSlop={12}
              style={styles.sendButton}
              accessibilityLabel="Send"
              accessibilityState={{ disabled: !canSend }}
            />
          )}
        </View>
      </View>

      {/* iOS pushed the mention picker as a formSheet route; here the
       *  composer mounts the same picker body inside a BottomSheet. The
       *  sheet talks to the composer through useMentionDraftStore only. */}
      <MentionPickerSheet
        visible={mentionSheetVisible}
        onClose={() => setMentionSheetVisible(false)}
        mode={mentionPickerMode}
      />
    </View>
  );

  const body = expanded ? expandedContent : pillContent;

  // When the parent owns keyboard handling (chat wraps in
  // KeyboardAvoidingView + SafeAreaView), skip the keyboard-aware lift —
  // double-stacking causes the composer to jump twice on keyboard show.
  // iOS used KeyboardStickyView (react-native-keyboard-controller); the
  // JS KeyboardAvoidingView "padding" behavior is the platform-agnostic
  // equivalent and needs no native module on this platform.
  if (!manageKeyboard) return body;

  return (
    <View style={{ paddingBottom: keyboardHeight }}>
      {body}
    </View>
  );
}

const styles = StyleSheet.create({
  // border-t border-border bg-background px-3 pt-2
  pillWrap: {
    borderTopWidth: 1,
    paddingHorizontal: 12,
    paddingTop: 8,
  },
  // flex-row items-center gap-2 h-11 px-4 rounded-full bg-secondary
  pill: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    height: 44,
    paddingHorizontal: 16,
    borderRadius: 999,
  },
  pressed: { opacity: 0.8 },
  // text-base
  pillLabel: { fontSize: 16 },
  // bg-background px-3 pt-2 gap-2
  expandedWrap: {
    paddingHorizontal: 12,
    paddingTop: 8,
    gap: 8,
  },
  // px-3 py-1.5 rounded-md gap-0.5
  replyChip: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 6,
    gap: 2,
  },
  replyRow: { flexDirection: "row", alignItems: "center", gap: 8 },
  // flex-1 text-xs font-medium
  replyName: { flex: 1, fontSize: 12, fontWeight: "500" },
  replyCancel: { height: 16, width: 16, alignItems: "center", justifyContent: "center" },
  // text-xs pl-5
  replyPreview: { fontSize: 12, paddingLeft: 20 },
  // rounded-2xl border border-border bg-secondary
  card: {
    borderRadius: 16,
    borderWidth: 1,
    overflow: "hidden",
  },
  // px-2 pt-2 pb-1
  chipRowWrap: { paddingHorizontal: 8, paddingTop: 8, paddingBottom: 4 },
  // px-4 pt-3 pb-1
  input: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 4 },
  // flex-row items-center px-2 pb-2 pt-1
  toolbarRow: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 8,
    paddingBottom: 8,
    paddingTop: 4,
  },
  // h-8 w-8 (overrides the 40x40 icon Button size)
  toolbarButton: { width: 32, height: 32 },
  toolbarSpacer: { flex: 1 },
  // h-8 w-8 rounded-full
  sendButton: { width: 32, height: 32, borderRadius: 999 },
});
