"use client";

import type { ReactNode } from "react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useUploadGate,
} from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { ChatAddMenu } from "./chat-add-menu";
import { useChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";
import { formatShortcut, useShortcut } from "@multica/core/shortcuts";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";
import type { Attachment } from "@multica/core/types";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");
const EMPTY_ATTACHMENTS: Attachment[] = [];
/** Editor identity for the chat composer — see the editorKey note below. */
const CHAT_COMPOSER_EDITOR_KEY = "chat-composer";

function attachmentReferenceUrls(attachment: Attachment): string[] {
  const withUploadFields = attachment as Attachment & {
    markdownLink?: string;
    link?: string;
  };
  return [
    withUploadFields.markdownLink,
    attachment.markdown_url,
    attachment.download_url,
    attachment.url,
    withUploadFields.link,
    attachment.id ? `/api/attachments/${attachment.id}/download` : "",
  ].filter((url): url is string => !!url);
}

function isAttachmentReferenced(content: string, attachment: Attachment): boolean {
  return attachmentReferenceUrls(attachment).some((url) => content.includes(url));
}

interface ChatInputProps {
  onSend: (
    content: string,
    attachmentIds: string[] | undefined,
    commitInput: (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
    draftAttachments: Attachment[],
  ) => void | boolean | Promise<void | boolean>;
  restoreDraftRequest?: {
    id: string;
    content: string;
    attachments?: Attachment[];
    /**
     * Draft slot this restore targets. When set, the restore only fires while
     * the user is viewing that session — a fire-and-forget send that later
     * fails restores into the session it was sent from, not whatever the user
     * navigated to. Omit to restore into the current draft (legacy behavior).
     */
    sessionId?: string;
  } | null;
  /**
   * Fired when — and only when — the restore's content/attachments were written
   * into the draft. A restore the composer cannot apply yet (the user has work
   * in progress) is NOT reported: it stays pending and lands as soon as the
   * draft is clear. Owners holding a durable server-side restore (#5219) can
   * therefore treat this as the single terminal transition and consume the row.
   */
  onRestoreDraftApplied?: () => void;
  /** Receives a File and returns the attachment row (with id + CDN link).
   *  The wrapper owner (ChatWindow) lazy-creates a chat_session if needed
   *  and forwards `chatSessionId` to the upload — chat-input only cares
   *  about the upload result so it can map URL → id for back-fill on send.
   *  When unset, paste/drag/button still type into the editor but no upload
   *  fires (the editor's file-upload extension is a no-op without a handler). */
  onUploadFile?: (file: File) => Promise<UploadResult | null>;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
  /** True when the user has no agent available — disables the editor and
   *  surfaces a distinct placeholder. Kept separate from `disabled` so
   *  archived-session copy stays untouched. */
  noAgent?: boolean;
  /** True when `disabled` is because the bound agent was archived (retired),
   *  as opposed to the session itself being archived — swaps the placeholder
   *  copy so the read-only reason reads accurately. */
  agentArchived?: boolean;
  /** Name of the currently selected agent, used in the placeholder. */
  agentName?: string;
  /** Rendered at the bottom-left of the input bar — typically the agent picker. */
  leftAdornment?: ReactNode;
  /** Chat @ suggestions: current/recent issue/project entries. */
  contextItems?: MentionItem[];
  /** Monotonic nonce bumped by the owner whenever the compose box should grab
   *  keyboard focus — currently on "new chat" so the user can type right away.
   *  0 (the initial value) is inert, so a plain deep-link open never steals
   *  focus; only an explicit bump does. */
  focusRequest?: number;
  /**
   * Optional storage/identity isolation for embedded chat surfaces that use
   * the shared composer without participating in the global chat selection
   * store (for example Agent Builder).
   */
  draftKeyOverride?: string;
  editorKeyOverride?: string;
}

export function ChatInput({
  onSend,
  restoreDraftRequest,
  onRestoreDraftApplied,
  onUploadFile,
  onStop,
  isRunning,
  disabled,
  noAgent,
  agentArchived,
  agentName,
  leftAdornment,
  contextItems,
  focusRequest,
  draftKeyOverride,
  editorKeyOverride,
}: ChatInputProps) {
  const { t } = useT("chat");
  const { t: tEditor } = useT("editor");
  const sendShortcut = useShortcut("send");
  const editorRef = useRef<ContentEditorRef>(null);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  // Two keys with deliberately different concerns:
  //
  // `draftKey` — zustand storage key. Scopes the in-progress draft per session
  // so different sessions don't bleed text into each other. An uncreated chat
  // uses ONE slot per workspace, deliberately NOT keyed by agent: the composer
  // is "the chat I have not created yet", and `selectedAgentId` only decides
  // where the first send goes (MUL-4864). This is a STORAGE key, not a React
  // identity.
  //
  // `editorKey` — React `key` on the ContentEditor, i.e. editor identity. It is
  // constant for the chat composer, because nothing about switching what you
  // are composing to should throw away the instance you are typing in:
  //   - Agent switch: same draft slot now, so a remount would only serve to
  //     drop the last <100ms of typing the draft debounce has not persisted.
  //   - Placeholder: ContentEditor's placeholder-sync effect refreshes it live,
  //     so it never needed a remount.
  //   - Draft restore (a cancelled run, a failed send): writes into
  //     `inputDraft`, and the editor's synchronized-value effect pushes it into
  //     the live instance. There is no second copy to drift or resurface.
  //   - Session switch / lazy create: when the user uploads a file in a
  //     brand-new chat, `handleUploadFile` awaits `ensureSession`, which flips
  //     `activeSessionId` from null → uuid mid-upload. A session-keyed editor
  //     would unmount right as the blob preview landed, dropping the image node
  //     before file-upload.ts could swap in the CDN URL — the user would watch
  //     the image flash on and vanish. Stable identity is what makes
  //     first-upload-creates-session behave like every later upload.
  // Embedded surfaces (Agent Builder) still pass `editorKeyOverride` to isolate
  // their own composer.
  const draftKey = draftKeyOverride ?? activeSessionId ?? DRAFT_NEW_SESSION;
  // Select a primitive — empty-string fallback keeps referential stability.
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? "");
  const draftAttachments = useChatStore(
    (s) => s.inputDraftAttachments[draftKey] ?? EMPTY_ATTACHMENTS,
  );
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const setInputDraftAttachments = useChatStore((s) => s.setInputDraftAttachments);
  const addInputDraftAttachment = useChatStore((s) => s.addInputDraftAttachment);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());
  const [isSubmitting, setIsSubmitting] = useState(false);
  // `isEmpty` tracks the LIVE editor, which the persisted draft lags by a
  // debounce, so the send affordance cannot be derived from `inputDraft` alone.
  // But `isEmpty` is never re-derived when the composer switches draft slots
  // either: ChatInput does not remount on a session switch, and ContentEditor's
  // synchronized-value effect pushes the incoming draft in with `emitUpdate: false`, so
  // no onUpdate fires. Read BOTH signals — a draft `isEmpty` has not seen yet (a
  // restored or parked one, or any persisted draft the user typed in another
  // session) still enables the button. A false enable costs nothing: handleSend
  // reads the live editor and bails when it is empty.
  const hasNothingToSend = isEmpty && !inputDraft.trim();
  const appliedRestoreIdRef = useRef<string | null>(null);
  const editorKey = editorKeyOverride ?? CHAT_COMPOSER_EDITOR_KEY;

  // The draft whose document the editor instance is currently HOLDING.
  //
  // Normally identical to `draftKey`. The two diverge only while an in-flight
  // upload pins the instance to the source document: ContentEditor's Guard 0
  // refuses to `setContent` over an `uploading` node, because wiping that node
  // strands the upload's finalize and the file silently disappears. Until the
  // upload settles the composer still shows — and still edits — the source
  // draft, so every byte the instance produces belongs to THIS key, not to
  // wherever the user has since navigated.
  //
  // `draftKey` answers "what is selected"; this answers "what is loaded". Every
  // write the editor drives must use the latter.
  const editorDraftKeyRef = useRef(draftKey);

  // Write a document into a draft slot: text, plus a prune of the attachments
  // its body no longer references (deleting an image's markdown drops the
  // staged upload with it). Shared by the live `onUpdate` and the draft-switch
  // flush below, so a document can never be filed under one rule by one path
  // and a different rule by the other. Attachments are read live for `key`
  // rather than passed in — during a divergence the rendered `draftAttachments`
  // belongs to the selected draft, not the loaded one.
  const commitDraft = useCallback(
    (key: string, markdown: string) => {
      setInputDraft(key, markdown);
      const attachments =
        useChatStore.getState().inputDraftAttachments[key] ?? EMPTY_ATTACHMENTS;
      if (attachments.length === 0) return;
      const referenced = attachments.filter((attachment) =>
        isAttachmentReferenced(markdown, attachment),
      );
      if (referenced.length !== attachments.length) {
        setInputDraftAttachments(key, referenced);
      }
    },
    [setInputDraft, setInputDraftAttachments],
  );
  // Submit gate. `uploading` disables the SubmitButton the instant an upload
  // starts; `isBlocked()` is re-read inside handleSend for the paths that skip
  // the button entirely (Mod+Enter mid-paste, drag-drop racing the keyboard).
  // Both read the editor document, which is the actual upload queue — this
  // used to be a local in-flight counter that a manual delete of the pending
  // image would leave stuck (MUL-4808).
  const uploadGate = useUploadGate(editorRef);

  // Move the editor from the draft it holds to the draft that is selected.
  //
  // Two hazards make this more than "let the value sync handle it":
  //
  // 1. Unflushed keystrokes. `onUpdate` is debounced, and by the time a
  //    debounce armed under draft A fires, `onUpdate` resolves to the latest
  //    render's closure — it would file A's document under B. Flushing takes
  //    those bytes back and commits them to the key they were typed in.
  // 2. An in-flight upload. Guard 0 pins the document (see editorDraftKeyRef),
  //    so the switch cannot happen yet at all: we leave BOTH the document and
  //    its writes on the source key and retry when the gate clears
  //    (`uploadGate.uploading` is a dep). Blocking navigation instead would be
  //    a worse trade — the user waits on a network round-trip to change tabs.
  //
  // Case 2 also has to force the adopt afterwards: ContentEditor's sync effect
  // CONSUMED the value change while Guard 0 was up (lastSyncedValueRef
  // advances before the guard), so it will never re-run for that value and the
  // target draft would never load on its own.
  //
  // useLayoutEffect, not useEffect, for two ordering reasons: passive effects
  // run child-first, so a passive flush here would land AFTER ContentEditor's
  // sync effect had already skipped on a dirty editor; and a layout effect is
  // part of the commit, so no pending debounce timer can fire ahead of it.
  const deferredAdoptRef = useRef(false);
  useLayoutEffect(() => {
    const loadedKey = editorDraftKeyRef.current;
    if (loadedKey === draftKey) return;
    if (editorRef.current?.hasActiveUploads() === true) {
      // Pinned. Stay bound to the source draft until the upload settles.
      deferredAdoptRef.current = true;
      logger.debug("input.draft switch deferred by in-flight upload", {
        from: loadedKey,
        to: draftKey,
      });
      return;
    }
    const pending = editorRef.current?.flushPendingUpdate() ?? null;
    if (pending !== null) {
      logger.debug("input.draft flush on key change", { from: loadedKey, to: draftKey });
      commitDraft(loadedKey, pending);
    }
    editorDraftKeyRef.current = draftKey;
    if (!deferredAdoptRef.current) return;
    deferredAdoptRef.current = false;
    const incoming = useChatStore.getState().inputDrafts[draftKey] ?? "";
    logger.debug("input.draft adopting after upload settled", { key: draftKey });
    editorRef.current?.adoptContent(incoming);
    setIsEmpty(!incoming.trim());
  }, [draftKey, uploadGate.uploading, commitDraft]);

  // Maps "URL inserted into the editor" → "attachment row id" so that
  // on send we can ask the server to bind only the attachments still
  // referenced in the message body. Cleared after every send. Mirrors
  // the comment-input flow exactly. The map key MUST match what the
  // editor actually wrote into the markdown — that's `markdownLink`
  // (the stable per-attachment URL) for normal post-MUL-3130 uploads
  // and `link` (= att.url) for the no-workspace upload branch where
  // there's no attachment-row id to address. Storing only `link` here
  // would cause `content.includes(url)` to miss every new chat upload
  // because the editor persists `markdownLink` instead, and the
  // `onSend` call would silently drop `attachment_ids` so the
  // attachment never binds to the chat message.
  const uploadMapRef = useRef<Map<string, string>>(new Map());

  // Grab keyboard focus when the owner bumps `focusRequest` (a new chat was
  // started) so the user can type immediately. The editor's `focus()` latches
  // through to `onCreate` when it isn't mounted yet, so this works even on the
  // first render of a freshly-mounted compose box. `0` is inert on purpose.
  useEffect(() => {
    if (!focusRequest) return;
    editorRef.current?.focus();
  }, [focusRequest]);

  useEffect(() => {
    if (!restoreDraftRequest) {
      appliedRestoreIdRef.current = null;
      return;
    }
    if (appliedRestoreIdRef.current === restoreDraftRequest.id) return;
    // Session-scoped restore: if this draft belongs to a specific session,
    // wait until the user is actually viewing it. A fire-and-forget send that
    // failed after the user navigated away must not dump its content into the
    // session they're now looking at — the request stays pending until they
    // return to the source session (draftKey then matches).
    if (restoreDraftRequest.sessionId && restoreDraftRequest.sessionId !== draftKey) {
      return;
    }
    // A draft with text OR staged attachments is user work in progress — never
    // overwrite either with a restore (the attachment write below replaces the
    // whole staged list). This is a WAIT, not a decision: the request stays
    // pending and this effect re-runs on every draft change, so the restore
    // lands as soon as the user sends or clears what they were typing. Marking
    // it done here would strand it for the rest of this composer's life.
    if (inputDraft.trim() || draftAttachments.length > 0) {
      logger.debug("input.restore waiting: draft has content", {
        draftKey,
        restoreId: restoreDraftRequest.id,
      });
      return;
    }
    appliedRestoreIdRef.current = restoreDraftRequest.id;
    setInputDraft(draftKey, restoreDraftRequest.content);
    setInputDraftAttachments(draftKey, restoreDraftRequest.attachments ?? []);
    setIsEmpty(!restoreDraftRequest.content.trim());
    onRestoreDraftApplied?.();
  }, [
    draftKey,
    inputDraft,
    draftAttachments,
    onRestoreDraftApplied,
    restoreDraftRequest,
    setInputDraft,
    setInputDraftAttachments,
  ]);

  const handleUpload = useCallback(
    async (file: File): Promise<UploadResult | null> => {
      if (!onUploadFile) return null;
      const result = await onUploadFile(file);
      if (result) {
        const persistedURL = result.markdownLink || result.link;
        uploadMapRef.current.set(persistedURL, result.id);
        // Bind to the draft this file is landing IN. The editor inserts the
        // node into whatever document it currently holds, so while an earlier
        // upload pins it to the source draft, that — not the selected draft —
        // is where the body lives. Filing the row anywhere else splits a
        // message from its own attachment.
        if (result.id) addInputDraftAttachment(editorDraftKeyRef.current, result);
      }
      return result;
    },
    [addInputDraftAttachment, onUploadFile],
  );

  // Drop zone wraps the rounded card so a drop anywhere on the input
  // surface routes the file through the editor's upload extension (same
  // handler as the in-editor paste path).
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const handleSend = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || isRunning || isSubmitting || disabled || noAgent) {
      logger.debug("input.send skipped", {
        emptyContent: !content,
        isRunning,
        isSubmitting,
        disabled,
        noAgent,
      });
      return;
    }
    // Block the send while any file is still uploading. If we let it
    // through the attachment id is not yet in uploadMapRef (the upload
    // resolves later) and the attachment would only end up bound to the
    // session, not the message — the agent then can't `multica attachment
    // download <id>` the file. The SubmitButton is also disabled in this
    // state via `uploading`, but Mod+Enter bypasses the button so we
    // still gate here.
    if (uploadGate.isBlocked()) {
      logger.debug("input.send skipped: uploads in flight");
      return;
    }
    // The editor is still holding a DIFFERENT draft's document than the one
    // selected — an upload pinned it and the adopt below has not run yet. The
    // upload gate above covers most of that window, but not the sliver between
    // the upload's final dispatch (node gone) and the re-render that adopts:
    // React flushes that state update on a scheduler task, so a click can land
    // first. Sending here would post the source draft's text into the selected
    // session and then clear the wrong draft.
    if (editorDraftKeyRef.current !== draftKey) {
      logger.debug("input.send skipped: composer still holds another draft", {
        loaded: editorDraftKeyRef.current,
        selected: draftKey,
      });
      return;
    }
    // Only send attachment IDs for uploads still present in the content.
    // Edits / deletions that remove the markdown URL also drop the binding.
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    for (const attachment of draftAttachments) {
      if (isAttachmentReferenced(content, attachment)) activeIds.push(attachment.id);
    }
    const uniqueActiveIds = Array.from(new Set(activeIds));
    // Capture draft key BEFORE onSend — creating a new session mutates
    // activeSessionId synchronously, so reading it after onSend would point
    // at the new session and leave the old draft orphaned.
    const keyAtSend = draftKey;
    let committed = false;
    const commitInput = (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => {
      if (committed) return;
      committed = true;
      // `clearEditor === false` means the owner sent fire-and-forget while the
      // user had already navigated to another session. The editor instance is
      // shared across sessions, so it now shows (and the user may be typing
      // into) a DIFFERENT draft — clearing it or blurring would wipe that
      // visible input. Only scrub the editor when the user is still on the
      // session they sent from.
      if (options?.clearEditor !== false) {
        editorRef.current?.clearContent();
        // Drop focus so the caret doesn't keep blinking under the StatusPill /
        // streaming reply that's about to take over the user's attention. The
        // input is also `disabled` once isRunning flips, and a focused-but-
        // disabled editor reads as a stale cursor. We deliberately don't auto-
        // refocus on completion — that would interrupt the user if they're
        // selecting text from the assistant reply; one click to refocus is
        // a fair price for not stealing focus mid-action.
        editorRef.current?.blur();
        setIsEmpty(true);
      }
      // The sent draft's data is cleared regardless — the message is on its
      // way, so its persisted draft must not resurface.
      clearInputDraft(keyAtSend);
      for (const key of options?.extraDraftKeys ?? []) {
        if (key !== keyAtSend) clearInputDraft(key);
      }
      uploadMapRef.current.clear();
      setIsSubmitting(false);
    };
    logger.info("input.send", {
      contentLength: content.length,
      draftKey: keyAtSend,
      attachmentCount: uniqueActiveIds.length,
    });
    setIsSubmitting(true);
    let accepted: void | boolean;
    try {
      accepted = await onSend(
        content,
        uniqueActiveIds.length > 0 ? uniqueActiveIds : undefined,
        commitInput,
        draftAttachments.filter((attachment) => uniqueActiveIds.includes(attachment.id)),
      );
    } catch (err) {
      logger.warn("input.send failed", err);
      if (!committed) setIsSubmitting(false);
      return;
    }
    if (accepted === false) {
      if (!committed) setIsSubmitting(false);
      return;
    }
    if (!committed) commitInput();
  };

  const placeholder = noAgent
    ? t(($) => $.input.placeholder_no_agent)
    : disabled
      ? agentArchived
        ? t(($) => $.input.placeholder_archived_agent)
        : t(($) => $.input.placeholder_archived)
      : agentName
        ? t(($) => $.input.placeholder_named, { name: agentName })
        : t(($) => $.input.placeholder_default);

  const uploadEnabled = !!onUploadFile && !disabled && !noAgent;

  return (
    <div
      className={cn(
        // The composer grows with the draft up to half the surface it sits on
        // — a fixed 160px cap made long drafts unreadable in a five-line
        // porthole (MUL-5196). `max-h-[50%]` resolves against the chat
        // surface (floating window, chat tab, agent builder), all of which
        // give this wrapper a definite height, so the cap scales when the
        // user resizes or expands the window. The wrapper must be a flex
        // column for the card below to shrink into that cap instead of
        // spilling out of it.
        "flex max-h-[50%] min-h-0 flex-col px-5 pb-3 pt-0",
        // Outer wrapper carries the disabled cursor. Inner card sets
        // pointer-events-none, which suppresses hover (and therefore
        // any cursor of its own) — splitting the two layers lets hover
        // bubble back here so the browser actually reads cursor.
        noAgent && "cursor-not-allowed",
      )}
    >
      <div
        {...(uploadEnabled ? dropZoneProps : {})}
        className={cn(
          // max-h-96 is the absolute ceiling on top of the wrapper's 50%: on a
          // tall surface half the height is more composer than anyone reads at
          // once, and it keeps the cap finite if a future host ever mounts the
          // composer without a definite height (percentage max-height would
          // then resolve to none).
          "relative mx-auto flex min-h-16 max-h-96 w-full max-w-4xl flex-col rounded-lg border border-surface-border bg-surface pb-9 transition-[border-color,box-shadow] focus-within:border-brand focus-within:ring-2 focus-within:ring-ring/20",
          // Visual + interaction lock when there's no agent. We don't
          // toggle ContentEditor's editable mode (Tiptap can't switch
          // cleanly post-mount, and the prop has been removed); instead
          // we drop pointer events at the wrapper level so clicks miss
          // the editor entirely, and dim the surface so it reads as
          // "disabled" rather than "broken".
          noAgent && "pointer-events-none opacity-60",
        )}
        aria-disabled={noAgent || undefined}
      >
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            // See the editorKey / draftKey split note above — editor identity
            // intentionally tracks neither the session nor the agent.
            key={editorKey}
            ref={editorRef}
            value={inputDraft}
            placeholder={placeholder}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              // The LOADED key, not the selected one: while an upload pins the
              // document this fires for the source draft's body — including the
              // upload's own completion dispatch.
              commitDraft(editorDraftKeyRef.current, md);
            }}
            onSubmit={handleSend}
            onUploadFile={uploadEnabled ? handleUpload : undefined}
            onUploadingChange={uploadGate.onUploadingChange}
            attachments={draftAttachments}
            debounceMs={100}
            mentionMode={contextItems ? "context" : "default"}
            mentionContextItems={contextItems}
            enableSlashCommands
            // The bubble menu carries the only affordance that can strip
            // formatting — "Normal text" (setParagraph) plus the mark/list
            // toggles. Once a `# ` input rule or a Markdown/HTML paste turns a
            // line into a heading, chat has no other way to remove it, so
            // without the bubble menu formatting can be created but never
            // undone (MUL-5106).
            showBubbleMenu
          />
        </div>
        {(uploadEnabled || leftAdornment) && (
          <div className="absolute bottom-1.5 left-1.5 flex items-center gap-1">
            {uploadEnabled && (
              <ChatAddMenu
                onSelectFile={(file) => editorRef.current?.uploadFile(file)}
              />
            )}
            {leftAdornment}
          </div>
        )}
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
          <SubmitButton
            onClick={handleSend}
            disabled={hasNothingToSend || isSubmitting || !!disabled || !!noAgent}
            loading={isSubmitting}
            busy={uploadGate.uploading}
            running={isRunning}
            onStop={onStop}
            tooltip={uploadGate.uploading
              ? tEditor(($) => $.upload.in_progress)
              : sendShortcut
                ? `${t(($) => $.input.send_tooltip)} · ${formatShortcut(sendShortcut)}`
                : t(($) => $.input.send_tooltip)}
            ariaLabel={uploadGate.uploading
              ? tEditor(($) => $.upload.in_progress)
              : t(($) => $.input.send_tooltip)}
            stopTooltip={t(($) => $.input.stop_tooltip)}
            stopAriaLabel={t(($) => $.input.stop_tooltip)}
          />
        </div>
        {uploadEnabled && isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}
