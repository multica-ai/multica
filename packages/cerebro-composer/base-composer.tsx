"use client";

// Unified composer (FIR-1748). One implementation behind two presets:
//   - MessageComposer  → Chat, Channels, DMs and their threads (push/pull comms)
//   - CommentComposer  → issue comments (new + reply)
// Both build on the shared ContentEditor. The superset of behaviors that used
// to live in five separate composers (ChatInput, CommentInput, ReplyInput, the
// channel ThreadReplyInput, and the channel new-message reuse of CommentInput)
// is reconciled here and toggled by props, so each surface keeps the exact
// shipped behavior it had — pin (fixed / sticky-bottom), the trigger-target
// bar, the expand pill + height cap, draft persistence, dictation, slash
// commands and context mentions — without duplicating the wiring five times.

import {
  forwardRef,
  useCallback,
  useId,
  useImperativeHandle,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { ArrowUp, Loader2, Square } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import {
  ContentEditor,
  type ContentEditorProps,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "@multica/views/editor";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { PinButton, useFloatPosition, useInputPin } from "@multica/cerebro-pin-input";
import { ComposerExpandToggle, useComposerHeight } from "@multica/cerebro-ui";
import { DraftSavedHint } from "@multica/cerebro-comment-drafts";
import {
  EditorDictationMic,
  TranscribingSkeleton,
  type DictationStatus,
} from "@multica/cerebro-dictation";

/**
 * Draft persistence handle injected by the preset. Comment/channel surfaces
 * build it from `useCommentDraft` (localStorage per issue/channel/thread); chat
 * builds it from `useChatStore` (per session + agent). The base composer stays
 * agnostic to where the draft lives.
 */
export interface ComposerDraftHandle {
  defaultValue: string;
  save: (md: string) => void;
  clear: () => void;
  saved: boolean;
}

// MentionItem is derived from ContentEditor's own prop type so the package does
// not need a deep import into the editor internals.
export type ComposerMentionItem = NonNullable<
  ContentEditorProps["mentionContextItems"]
>[number];

// Pin behavior differs per surface: issue/channel top-level composers pin to a
// fixed position at the viewport bottom; thread replies use sticky-bottom so
// they float only once scrolled past. "none" disables the pin entirely.
export type ComposerPinMode = "none" | "fixed" | "sticky-bottom";

// Where the dictation mic lives. Chat mounts its own mic in the action row
// (so it can show the transcribing skeleton); other surfaces let ContentEditor
// render its corner mic; "off" hides it.
export type ComposerDictation = "action-row" | "corner" | "off";

const OVERLAY_BAND = 28; // px — clears the ~24px chip row anchored at top-1

export interface BaseComposerProps {
  /** Issue/channel id uploads attach to + ContentEditor context (sub-issue
   *  action). Omitted for chat, which has no backing issue. */
  uploadIssueId?: string;
  /** Remount key: switch session/channel/thread → fresh editor + draft seed. */
  editorKey?: string;
  /** Draft persistence handle (preset supplies the right store). */
  draft: ComposerDraftHandle;

  onSubmit: (content: string, attachmentIds?: string[]) => void | Promise<void>;
  /** Track upload ids and forward only those still present in the sent text.
   *  Comments/channels need this; chat passes content only (false). */
  trackAttachmentIds?: boolean;

  placeholder?: string;
  autoFocus?: boolean;
  disabled?: boolean;
  /** No agent available (chat) — locks the field and dims it. */
  noAgent?: boolean;

  // --- editor features ---
  enableSlashCommands?: boolean;
  showBubbleMenu?: boolean;
  mentionMode?: "default" | "context";
  mentionContextItems?: ComposerMentionItem[];
  onTyping?: () => void;

  // --- height cap / expand pill ---
  enableExpand?: boolean;

  // --- dictation ---
  dictation?: ComposerDictation;

  // --- overlays / issue-specific chrome (injected by CommentComposer) ---
  /** Rendered over the field's top band; receives the live markdown, a faded
   *  flag (true once the body scrolled under it), and an `insertText` helper
   *  so overlay actions (e.g. tag-owner) can write into the editor. */
  renderTopOverlay?: (args: {
    markdown: string;
    faded: boolean;
    insertText: (text: string) => void;
  }) => ReactNode;
  /** True while a top overlay occupies the band, so the body padding reserves
   *  room for it (matches the per-composer OVERLAY_BAND behavior). */
  hasTopOverlay?: boolean;
  /** Drop the placeholder while an overlay sits over the empty field. */
  suppressPlaceholder?: boolean;
  /** Confirm before send (foreign private agent). Returns false → abort. */
  confirmBeforeSend?: (content: string) => Promise<boolean>;
  confirmDialog?: ReactNode;

  // --- pin ---
  pin?: ComposerPinMode;

  // --- layout ---
  avatar?: { type: string; id: string; size?: number } | null;
  size?: "sm" | "default";

  // --- slots (chat) ---
  topSlot?: ReactNode;
  /** Rendered inside the framed box, directly under the input (FIR-1870 —
   *  the session context line lives under the comment input, inside the box). */
  footerSlot?: ReactNode;
  leftAdornment?: ReactNode;
  rightAdornment?: ReactNode;
  isRunning?: boolean;
  onStop?: () => void;

  /** Dictation action-row extras, supplied by MessageComposer for chat. */
  dictationRow?: ReactNode;
  /** Shown above the editor while dictation is transcribing (chat). */
  transcribingSlot?: ReactNode;

  /** Blur the editor after a successful send (chat — avoids a stale caret
   *  blinking under the streaming reply). */
  blurOnSubmit?: boolean;
  /** Draw the boxed field chrome (card surface + border ring). Default true
   *  (Channels, DMs, Chat). CommentComposer passes false so issue comment
   *  fields render without a box (FIR-1790). When the field floats while
   *  pinned, the box is restored regardless so it stays readable over the
   *  scrolling content behind it. */
  frame?: boolean;
  className?: string;
}

/** Imperative handle for surfaces that must write into the editor from outside
 *  the field (chat dictation inserts the transcript at the caret). */
export interface ComposerHandle {
  insertText: (text: string) => void;
  focus: () => void;
  blur: () => void;
}

export const BaseComposer = forwardRef<ComposerHandle, BaseComposerProps>(function BaseComposer({
  uploadIssueId,
  editorKey,
  draft,
  onSubmit,
  trackAttachmentIds = true,
  placeholder,
  autoFocus = false,
  disabled = false,
  noAgent = false,
  enableSlashCommands = false,
  showBubbleMenu,
  mentionMode = "default",
  mentionContextItems,
  onTyping,
  enableExpand = true,
  dictation = "corner",
  renderTopOverlay,
  hasTopOverlay = false,
  suppressPlaceholder = false,
  confirmBeforeSend,
  confirmDialog,
  pin = "none",
  avatar = null,
  size = "default",
  topSlot,
  footerSlot,
  leftAdornment,
  rightAdornment,
  isRunning = false,
  onStop,
  dictationRow,
  transcribingSlot,
  blurOnSubmit = false,
  frame = true,
  className,
}: BaseComposerProps, forwardedRef) {
  const editorRef = useRef<ContentEditorRef>(null);
  const anchorRef = useRef<HTMLDivElement>(null);

  useImperativeHandle(forwardedRef, () => ({
    insertText: (text: string) => editorRef.current?.insertText(text),
    focus: () => editorRef.current?.focus(),
    blur: () => editorRef.current?.blur(),
  }), []);
  const submitOnEnter = useSubmitOnEnter();
  const composerHeight = useComposerHeight();

  // Seed submit-enable + overlay markdown from the restored draft so a
  // recovered draft is immediately sendable without an extra keypress. The old
  // ChatInput initialised isEmpty from the draft (`!inputDraft.trim()`); the
  // unified field must too, or the editor shows the draft while Submit stays
  // disabled until the user types. ContentEditor does not emit onUpdate at
  // mount, so without this seed the gate never opens on its own.
  const [isEmpty, setIsEmpty] = useState(() => !draft.defaultValue.trim());
  const [markdown, setMarkdown] = useState(() => draft.defaultValue);
  const [submitting, setSubmitting] = useState(false);
  const [scrolled, setScrolled] = useState(false);

  // Dictation now lives in the bottom-LEFT cluster next to the attach button on
  // every text surface (FIR-1637, Jesper) — chat, thread replies and comments
  // all share the one pattern. When a preset doesn't supply its own dictation
  // wiring (chat does, for its skeleton), the composer mounts a default mic that
  // inserts the transcript at the editor caret. `internalTranscribing` drives
  // the transcribing skeleton for those default-mic surfaces.
  const [internalTranscribing, setInternalTranscribing] = useState(false);
  const handleInternalDictationStatus = useCallback((s: DictationStatus) => {
    setInternalTranscribing(s === "transcribing");
  }, []);

  // ContentEditor remounts on editorKey change (session / thread switch) and
  // re-seeds from the new draft, but BaseComposer itself does not unmount — so
  // re-sync the derived submit-enable/markdown to the new draft on key change,
  // else a draft restored under a new key would show with Submit still
  // disabled. (Adjust-state-during-render pattern, guarded by the key ref.)
  const prevEditorKeyRef = useRef(editorKey);
  if (prevEditorKeyRef.current !== editorKey) {
    prevEditorKeyRef.current = editorKey;
    setIsEmpty(!draft.defaultValue.trim());
    setMarkdown(draft.defaultValue);
  }

  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);

  const reactId = useId();
  const pinEnabledRequested = pin !== "none";
  const pinKey = `composer:${reactId}`;
  const { enabled: pinEnabled, isPinned, togglePin, unpin } = useInputPin(
    pinKey,
    pinEnabledRequested,
  );
  const floatRect = useFloatPosition(
    anchorRef,
    isPinned,
    pin === "sticky-bottom" ? { mode: "sticky-bottom" } : undefined,
  );
  const isFloating = floatRect !== null;

  // The visible box (card surface + border ring) is dropped for the unframed
  // comment composer (FIR-1790); Channels/DMs/Chat keep it via frame=true. A
  // fixed-pinned field floats over content, so restore the box while floating
  // regardless of the frame prop, otherwise it would be unreadable.
  const showFrame = frame || (isFloating && pin === "fixed");

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
    enabled: !disabled,
  });

  const handleUpload = useCallback(
    async (file: File) => {
      const result = await uploadWithToast(file, uploadIssueId ? { issueId: uploadIssueId } : undefined);
      if (result && trackAttachmentIds) {
        uploadMapRef.current.set(result.link, result.id);
      }
      return result;
    },
    [uploadWithToast, uploadIssueId, trackAttachmentIds],
  );

  const handleSubmit = async () => {
    const content = editorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!content || submitting || disabled || noAgent) return;
    if (confirmBeforeSend && !(await confirmBeforeSend(content))) return;

    let activeIds: string[] | undefined;
    if (trackAttachmentIds) {
      const ids: string[] = [];
      for (const [url, id] of uploadMapRef.current) {
        if (content.includes(url)) ids.push(id);
      }
      activeIds = ids.length > 0 ? ids : undefined;
    }

    setSubmitting(true);
    try {
      await onSubmit(content, activeIds);
      editorRef.current?.clearContent();
      draft.clear();
      setIsEmpty(true);
      setMarkdown("");
      uploadMapRef.current.clear();
      // Chat blurs after send so the caret doesn't blink under the streaming reply.
      if (blurOnSubmit) editorRef.current?.blur();
      if (isPinned) {
        unpin();
        requestAnimationFrame(() => {
          anchorRef.current?.scrollIntoView({ block: "end", behavior: "smooth" });
        });
      }
    } finally {
      setSubmitting(false);
    }
  };

  // Reserve a top band so a floating overlay (trigger bar) / expand pill sits
  // ABOVE the first lines of text instead of overlapping them, and grow the
  // collapsed cap by the same band.
  const showExpand = enableExpand && composerHeight.showExpandToggle;
  const hasOverlay = hasTopOverlay || showExpand;
  const baseStyle = composerHeight.containerStyle;
  const scrollStyle =
    hasOverlay && baseStyle
      ? {
          ...baseStyle,
          paddingTop: OVERLAY_BAND,
          ...(baseStyle.maxHeight != null
            ? { maxHeight: baseStyle.maxHeight + OVERLAY_BAND }
            : {}),
        }
      : baseStyle;

  const floatStyle = isPinned && floatRect
    ? ({
        position: "fixed" as const,
        bottom: 16,
        left: floatRect.left,
        width: floatRect.width,
        zIndex: 30,
      })
    : undefined;

  const avatarSize = avatar?.size ?? (size === "sm" ? 22 : 28);

  const editor = (
    <ContentEditor
      key={editorKey}
      ref={editorRef}
      placeholder={suppressPlaceholder ? "" : placeholder}
      defaultValue={draft.defaultValue}
      onUpdate={(md) => {
        setMarkdown(md);
        setIsEmpty(!md.trim());
        draft.save(md);
        if (md.trim()) onTyping?.();
      }}
      onSubmit={handleSubmit}
      onUploadFile={handleUpload}
      debounceMs={100}
      currentIssueId={uploadIssueId}
      submitOnEnter={submitOnEnter}
      autoFocus={autoFocus && !disabled && !noAgent}
      mentionMode={mentionMode}
      mentionContextItems={mentionContextItems}
      enableSlashCommands={enableSlashCommands}
      showBubbleMenu={showBubbleMenu}
      hideDictationMic
    />
  );

  // Attachment always sits in the bottom-LEFT cluster (next to any preset
  // leftAdornment), never in the right action row beside Submit. Consistent
  // across every surface (Channels, DM, Chat, comments) — FIR-1748 follow-up.
  const attachButton = (
    <FileUploadButton
      size="sm"
      multiple
      disabled={disabled}
      onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
      onEmbed={(files) =>
        files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))
      }
    />
  );

  // The dictation mic sits next to the attach button. Chat supplies its own
  // `dictationRow` (wired to its skeleton + ref); every other surface gets the
  // default mic that writes the transcript straight into this editor.
  const dictationMic =
    dictation === "off"
      ? null
      : (dictationRow ?? (
          <EditorDictationMic
            disabled={disabled}
            onTranscribed={(text) => editorRef.current?.insertText(text)}
            onStatusChange={handleInternalDictationStatus}
          />
        ));

  const actionRow = (
    <>
      {rightAdornment}
      <DraftSavedHint show={draft.saved} className="mr-1" />
      {pinEnabled && (
        <PinButton
          isPinned={isPinned}
          onToggle={togglePin}
          pinLabel="Pin"
          unpinLabel="Unpin"
        />
      )}
      {isRunning && onStop ? (
        <Button size="icon-sm" variant="secondary" onClick={onStop} aria-label="Stop">
          <Square className="fill-current" />
        </Button>
      ) : null}
      <Button
        size="icon-sm"
        aria-label="Submit"
        disabled={isEmpty || submitting || disabled || noAgent}
        onClick={handleSubmit}
      >
        {submitting ? <Loader2 className="animate-spin" /> : <ArrowUp />}
      </Button>
    </>
  );

  return (
    <div ref={anchorRef}>
      {confirmDialog}
      {isFloating && pin === "sticky-bottom" && (
        <div aria-hidden style={{ height: floatRect?.height }} />
      )}
      <div
        style={floatStyle}
        className={cn(
          isFloating && pin === "sticky-bottom" &&
            "rounded-lg border border-border bg-background p-2 shadow-lg",
        )}
      >
        <div className={cn("flex items-start gap-2.5", className)}>
          {avatar && (
            <ActorAvatar
              actorType={avatar.type}
              actorId={avatar.id}
              size={avatarSize}
              className="mt-0.5 shrink-0 hidden sm:block"
            />
          )}
          <div className="min-w-0 flex-1">
            <div
              {...dropZoneProps}
              data-testid="composer-input"
              onMouseDown={(e) => {
                if ((e.target as HTMLElement).closest("button, a, input, textarea, [contenteditable]")) return;
                e.preventDefault();
                editorRef.current?.focus();
              }}
              className={cn(
                "relative flex flex-col pb-8 min-h-20",
                showFrame && "rounded-lg bg-card ring-1 ring-border",
                noAgent && "pointer-events-none opacity-60",
                isPinned && pin === "fixed" && "ring-emerald-500/40 shadow-lg",
              )}
              aria-disabled={noAgent || undefined}
            >
              {topSlot}
              {transcribingSlot ??
                (internalTranscribing ? <TranscribingSkeleton /> : null)}
              {renderTopOverlay?.({
                markdown,
                faded: scrolled,
                insertText: (text) => editorRef.current?.insertText(text),
              })}
              {showExpand && (
                <ComposerExpandToggle
                  isExpanded={composerHeight.isExpanded}
                  onToggle={() => {
                    composerHeight.toggleExpanded();
                    editorRef.current?.focus();
                  }}
                  expandLabel="Expand"
                  collapseLabel="Collapse"
                  faded={scrolled}
                />
              )}
              <div
                ref={composerHeight.scrollRef}
                className="flex-1 min-h-0 overflow-y-auto px-3 py-2"
                style={scrollStyle}
                onScroll={(e) => setScrolled(e.currentTarget.scrollTop > 2)}
              >
                {editor}
              </div>
              {/* FIR-1870: session context line — directly under the input, inside the box, above the action toolbar. */}
              {footerSlot ? <div className="px-3">{footerSlot}</div> : null}
              <div className="absolute bottom-1 left-1.5 flex items-center gap-1">
                {attachButton}
                {dictationMic}
                {leftAdornment}
              </div>
              <div className="absolute bottom-1 right-1.5 flex items-center gap-2 sm:gap-1">
                {actionRow}
              </div>
              {isDragOver && <FileDropOverlay />}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
});
