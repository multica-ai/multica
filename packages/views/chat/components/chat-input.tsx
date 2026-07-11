"use client";

import type { ReactNode } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { ChatAddMenu } from "./chat-add-menu";
import { useChatStore, newSessionDraftKey } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";
import type { Attachment } from "@multica/core/types";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");
const EMPTY_ATTACHMENTS: Attachment[] = [];

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
  onRestoreDraftConsumed?: () => void;
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
}

export function ChatInput({
  onSend,
  restoreDraftRequest,
  onRestoreDraftConsumed,
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
}: ChatInputProps) {
  const { t } = useT("chat");
  const editorRef = useRef<ContentEditorRef>(null);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  // Two keys with deliberately different concerns:
  //
  // `draftKey` — zustand storage key. Scopes the in-progress draft per
  // session so different sessions don't bleed text into each other; for
  // brand-new chats it falls back to a per-agent slot so switching agents
  // mid-compose gives each agent its own draft. This is a STORAGE key, not
  // a React identity.
  //
  // `editorKey` — React `key` on the ContentEditor. Forces a fresh editor
  // instance when the user explicitly switches agent. Placeholder text itself
  // no longer depends on this: ContentEditor's placeholder-sync effect
  // refreshes it live (e.g. across archived ↔ active sessions of the SAME
  // agent, where this key does not change). A cancelled-run draft restore
  // does NOT bump this key either: it just writes
  // the restored text into `inputDraft`, and the editor's own
  // defaultValue-sync effect (content-editor.tsx) pushes it into the live
  // instance. There is no second copy of the draft to drift or resurface.
  // Crucially this does NOT include `activeSessionId`: when the user
  // uploads a file in a brand-new chat, `handleUploadFile` first awaits
  // `ensureSession` which lazily creates the session and flips
  // `activeSessionId` from null → uuid mid-upload. If the editor key
  // depended on session id, that flip would unmount the editor right as
  // the blob preview was inserted, dropping the in-progress upload's
  // image node before file-upload.ts could swap it for the CDN URL — the
  // user would see the image flash on then disappear. Keeping editor
  // identity stable across the lazy-create event is what makes
  // first-upload-creates-session work the same as second-upload.
  const draftKey = activeSessionId ?? newSessionDraftKey(selectedAgentId);
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
  const consumedRestoreIdRef = useRef<string | null>(null);
  const editorKey = selectedAgentId ?? "no-agent";
  // Number of in-flight uploads. We track this explicitly (rather than
  // peeking at the editor on every render) so the SubmitButton visibly
  // disables the instant an upload starts and re-enables the instant it
  // finishes. handleSend ALSO checks `hasActiveUploads()` for paths that
  // bypass the button (Mod+Enter while paste is mid-stream, drag-drop
  // racing the keyboard) — defense in depth.
  const [pendingUploads, setPendingUploads] = useState(0);

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
      consumedRestoreIdRef.current = null;
      return;
    }
    if (consumedRestoreIdRef.current === restoreDraftRequest.id) return;
    // Session-scoped restore: if this draft belongs to a specific session,
    // wait until the user is actually viewing it. A fire-and-forget send that
    // failed after the user navigated away must not dump its content into the
    // session they're now looking at — the request stays pending until they
    // return to the source session (draftKey then matches).
    if (restoreDraftRequest.sessionId && restoreDraftRequest.sessionId !== draftKey) {
      return;
    }
    consumedRestoreIdRef.current = restoreDraftRequest.id;
    if (inputDraft.trim()) {
      logger.info("input.restore skipped: draft already has content", {
        draftKey,
        restoreId: restoreDraftRequest.id,
      });
      onRestoreDraftConsumed?.();
      return;
    }
    setInputDraft(draftKey, restoreDraftRequest.content);
    setInputDraftAttachments(draftKey, restoreDraftRequest.attachments ?? []);
    setIsEmpty(!restoreDraftRequest.content.trim());
    onRestoreDraftConsumed?.();
  }, [
    draftKey,
    inputDraft,
    onRestoreDraftConsumed,
    restoreDraftRequest,
    setInputDraft,
    setInputDraftAttachments,
  ]);

  const handleUpload = useCallback(
    async (file: File): Promise<UploadResult | null> => {
      if (!onUploadFile) return null;
      setPendingUploads((n) => n + 1);
      try {
        const result = await onUploadFile(file);
        if (result) {
          const persistedURL = result.markdownLink || result.link;
          uploadMapRef.current.set(persistedURL, result.id);
          if (result.id) addInputDraftAttachment(draftKey, result);
        }
        return result;
      } finally {
        setPendingUploads((n) => Math.max(0, n - 1));
      }
    },
    [addInputDraftAttachment, draftKey, onUploadFile],
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
    if (editorRef.current?.hasActiveUploads()) {
      logger.debug("input.send skipped: uploads in flight");
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
        "px-5 pb-3 pt-0",
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
          "relative mx-auto flex min-h-16 max-h-40 w-full max-w-4xl flex-col rounded-lg border border-surface-border bg-surface pb-9 transition-[border-color,box-shadow] focus-within:border-brand focus-within:ring-2 focus-within:ring-ring/20",
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
            // See the editorKey / draftKey split note above — editorKey
            // intentionally does not depend on activeSessionId.
            key={editorKey}
            ref={editorRef}
            defaultValue={inputDraft}
            placeholder={placeholder}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              setInputDraft(draftKey, md);
              if (draftAttachments.length > 0) {
                const referenced = draftAttachments.filter((attachment) =>
                  isAttachmentReferenced(md, attachment),
                );
                if (referenced.length !== draftAttachments.length) {
                  setInputDraftAttachments(draftKey, referenced);
                }
              }
            }}
            onSubmit={handleSend}
            onUploadFile={uploadEnabled ? handleUpload : undefined}
            attachments={draftAttachments}
            debounceMs={100}
            mentionMode={contextItems ? "context" : "default"}
            mentionContextItems={contextItems}
            enableSlashCommands
            // Chat is short-form — the floating formatting toolbar is
            // more distraction than feature here.
            showBubbleMenu={false}
            // Chat intentionally leaves submitOnEnter at its default false:
            // Mod+Enter submits, while bare Enter falls through to Tiptap's
            // default behavior for lists, quotes, and paragraph breaks.
            // Without this, Enter-as-send would steal the only key that
            // continues a bullet list, leaving users stuck after one item.
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
            shape="circle"
            onClick={handleSend}
            disabled={isEmpty || isSubmitting || !!disabled || !!noAgent || pendingUploads > 0}
            loading={isSubmitting}
            running={isRunning}
            onStop={onStop}
            tooltip={`${t(($) => $.input.send_tooltip)} · ${formatShortcut(modKey, enterKey)}`}
            stopTooltip={t(($) => $.input.stop_tooltip)}
          />
        </div>
        {uploadEnabled && isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}
