"use client";

import type { ReactNode } from "react";
import { useCallback, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useChatStore, DRAFT_NEW_SESSION } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");

interface ChatInputProps {
  onSend: (content: string, attachmentIds?: string[]) => void;
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
  /** Name of the currently selected agent, used in the placeholder. */
  agentName?: string;
  /** Rendered at the bottom-left of the input bar — typically the agent picker. */
  leftAdornment?: ReactNode;
  /** Rendered just before the submit button — used for context-anchor action. */
  rightAdornment?: ReactNode;
  /** Rendered inside the rounded container, above the editor — attached
   *  context cards, drafts, etc. */
  topSlot?: ReactNode;
}

export function ChatInput({
  onSend,
  onUploadFile,
  onStop,
  isRunning,
  disabled,
  noAgent,
  agentName,
  leftAdornment,
  rightAdornment,
  topSlot,
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
  // `editorKey` — React `key` on the ContentEditor. Used ONLY to force a
  // remount when the user explicitly switches agent (so Tiptap's
  // Placeholder, which only reads on mount, refreshes to "Tell {agent}…").
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
  const draftKey =
    activeSessionId ?? `${DRAFT_NEW_SESSION}:${selectedAgentId ?? ""}`;
  const editorKey = selectedAgentId ?? "no-agent";
  // Select a primitive — empty-string fallback keeps referential stability.
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? "");
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());
  // Number of in-flight uploads. We track this explicitly (rather than
  // peeking at the editor on every render) so the SubmitButton visibly
  // disables the instant an upload starts and re-enables the instant it
  // finishes. handleSend ALSO checks `hasActiveUploads()` for paths that
  // bypass the button (Mod+Enter while paste is mid-stream, drag-drop
  // racing the keyboard) — defense in depth.
  const [pendingUploads, setPendingUploads] = useState(0);

  // Maps "CDN URL inserted into the editor" → "attachment row id" so that
  // on send we can ask the server to bind only the attachments still
  // referenced in the message body. Cleared after every send. Mirrors the
  // comment-input flow exactly.
  const uploadMapRef = useRef<Map<string, string>>(new Map());

  const handleUpload = useCallback(
    async (file: File): Promise<UploadResult | null> => {
      if (!onUploadFile) return null;
      setPendingUploads((n) => n + 1);
      try {
        const result = await onUploadFile(file);
        if (result) uploadMapRef.current.set(result.link, result.id);
        return result;
      } finally {
        setPendingUploads((n) => Math.max(0, n - 1));
      }
    },
    [onUploadFile],
  );

  // Drop zone wraps the rounded card so a drop anywhere on the input
  // surface routes the file through the editor's upload extension (same
  // handler as the in-editor paste path).
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || isRunning || disabled || noAgent) {
      logger.debug("input.send skipped", {
        emptyContent: !content,
        isRunning,
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
    // Capture draft key BEFORE onSend — creating a new session mutates
    // activeSessionId synchronously, so reading it after onSend would point
    // at the new session and leave the old draft orphaned.
    const keyAtSend = draftKey;
    logger.info("input.send", {
      contentLength: content.length,
      draftKey: keyAtSend,
      attachmentCount: activeIds.length,
    });
    onSend(content, activeIds.length > 0 ? activeIds : undefined);
    editorRef.current?.clearContent();
    // Drop focus so the caret doesn't keep blinking under the StatusPill /
    // streaming reply that's about to take over the user's attention. The
    // input is also `disabled` once isRunning flips, and a focused-but-
    // disabled editor reads as a stale cursor. We deliberately don't auto-
    // refocus on completion — that would interrupt the user if they're
    // selecting text from the assistant reply; one click to refocus is
    // a fair price for not stealing focus mid-action.
    editorRef.current?.blur();
    clearInputDraft(keyAtSend);
    uploadMapRef.current.clear();
    setIsEmpty(true);
  };

  const placeholder = noAgent
    ? t(($) => $.input.placeholder_no_agent)
    : disabled
      ? t(($) => $.input.placeholder_archived)
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
          "relative mx-auto flex min-h-16 max-h-40 w-full max-w-4xl flex-col rounded-lg bg-card pb-9 border-1 border-border transition-colors focus-within:border-brand",
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
        {topSlot}
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
            }}
            onSubmit={handleSend}
            onUploadFile={uploadEnabled ? handleUpload : undefined}
            debounceMs={100}
            // Chat is short-form — the floating formatting toolbar is
            // more distraction than feature here.
            showBubbleMenu={false}
            // Mod+Enter submits. Bare Enter falls through to Tiptap's
            // default, which continues lists/quotes and breaks paragraphs.
            // Without this, Enter-as-send would steal the only key that
            // continues a bullet list, leaving users stuck after one item.
          />
        </div>
        {leftAdornment && (
          <div className="absolute bottom-1.5 left-2 flex items-center">
            {leftAdornment}
          </div>
        )}
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
          {rightAdornment}
          {uploadEnabled && (
            <FileUploadButton
              size="sm"
              onSelect={(file) => editorRef.current?.uploadFile(file)}
            />
          )}
          <SubmitButton
            onClick={handleSend}
            disabled={isEmpty || !!disabled || !!noAgent || pendingUploads > 0}
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
