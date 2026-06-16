"use client";

// CEREBRO-PATCH(comment-input-cerebro): cerebro modification of upstream file
// CEREBRO-PATCH(comment-input-pin): JEH-1065 — opt-in `pinnable` prop. When
// set (issue pages do, channels/DMs stay opt-out), the input gains a pin
// toggle that pins the field to the bottom of the viewport via inline
// `position: fixed`. The editor stays in the same React tree slot on every
// render — only the wrapper style differs — so a Tiptap draft / caret /
// pending upload survives the pin toggle. Auto-unpins on submit.

import { useCallback, useId, useRef, useState } from "react";
import { ArrowUp, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef } from "../../editor/content-editor";
import { FileDropOverlay } from "../../editor/file-drop-overlay";
import { useFileDropZone } from "../../editor/use-file-drop-zone";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { PinButton, useFloatPosition, useInputPin } from "@multica/cerebro-pin-input";
import { useT } from "../../i18n";
// CEREBRO-PATCH(reply-target-agent-indicator): FIR-2392 — top-level composer
// shows the same "who will be triggered" bar as the reply composer so the
// indicator follows the trigger logic everywhere.
import { TriggerTargetBar, memberMentionMarkdown } from "./trigger-target-bar";
// CEREBRO-PATCH(private-agent-send-confirm): FIR-32 — confirm before waking a foreign private agent.
import { usePrivateAgentSendConfirm } from "@multica/cerebro-access/views";
// CEREBRO-PATCH(comment-drafts): TECH-3491 — per-device draft persistence for the new-comment / channel / DM composer.
import { useCommentDraft, DraftSavedHint } from "@multica/cerebro-comment-drafts";
// CEREBRO-PATCH(composer-height-cap): TECH-3536 — cap the field at 50% of the space above the mobile keyboard, with an expand-to-80% pill.
import { ComposerExpandToggle, useComposerHeight } from "@multica/cerebro-ui";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  // CEREBRO-PATCH(input-autofocus): JEH-756 — opt-in autofocus for surfaces
  // where the user expects to start typing on entry (channels, DMs). Issue
  // pages stay opt-out: opening an issue is read-first.
  autoFocus?: boolean;
  /**
   * CEREBRO-PATCH(comment-input-pin): JEH-1065 — opt in to the pin toggle.
   * Issue pages pass `true`; channels and DMs leave it off so the chat-style
   * surfaces stay unchanged.
   */
  pinnable?: boolean;
  /**
   * CEREBRO-PATCH(reply-target-agent-indicator): FIR-2392 — agent the
   * backend trigger will wake when the comment has no @agent mentions.
   * Forwarded from `issue-detail.tsx` (resolved from issue assignee or
   * squad leader). Channels/DMs pass nothing — the bar stays hidden there.
   */
  triggerAgentId?: string;
}

function CommentInput({ issueId, onSubmit, autoFocus = false, pinnable = false, triggerAgentId }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const submitOnEnter = useSubmitOnEnter();
  // CEREBRO-PATCH(composer-height-cap): TECH-3536 — mobile height cap + expand state.
  const composerHeight = useComposerHeight();
  const [isEmpty, setIsEmpty] = useState(true);
  const [markdown, setMarkdown] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  // CEREBRO-PATCH(private-agent-send-confirm): FIR-32 — gate send on a foreign-private-agent confirm.
  const { confirmBeforeSend, confirmDialog } = usePrivateAgentSendConfirm({ triggerAgentId });
  // CEREBRO-PATCH(comment-drafts): TECH-3491 — persist this composer's unsent text (key per issue/channel/DM).
  const draft = useCommentDraft(`new:${issueId}`);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const reactId = useId();
  const pinKey = `comment:${issueId}:${reactId}`;
  const { enabled: pinEnabled, isPinned, togglePin, unpin } = useInputPin(pinKey, pinnable);
  const floatRect = useFloatPosition(anchorRef, isPinned);

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      uploadMapRef.current.set(result.link, result.id);
    }
    return result;
  }, [uploadWithToast, issueId]);

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // CEREBRO-PATCH(private-agent-send-confirm): FIR-32 — stop here if the user cancels the confirm.
    if (!(await confirmBeforeSend(content))) return;
    // Only send attachment IDs for uploads still present in the content.
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      draft.clear(); // CEREBRO-PATCH(comment-drafts): TECH-3491 — sent, drop the stored draft.
      setIsEmpty(true);
      setMarkdown("");
      uploadMapRef.current.clear();
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

  const floatStyle = isPinned && floatRect
    ? ({
        position: "fixed" as const,
        bottom: 16,
        left: floatRect.left,
        width: floatRect.width,
        zIndex: 30,
      })
    : undefined;

  return (
    <div ref={anchorRef}>
      {/* CEREBRO-PATCH(private-agent-send-confirm): FIR-32 — portals itself; renders nothing until a send needs confirming. */}
      {confirmDialog}
      {isPinned && (
        <div className="flex items-center justify-between gap-2 rounded-lg border border-dashed border-emerald-500/40 bg-emerald-500/5 px-3 py-2 text-xs text-muted-foreground">
          <span className="truncate">{t(($) => $.comment.pinned_placeholder)}</span>
          <PinButton
            isPinned
            onToggle={togglePin}
            pinLabel={t(($) => $.comment.pin_tooltip)}
            unpinLabel={t(($) => $.comment.unpin_tooltip)}
            className="shrink-0"
          />
        </div>
      )}
      <div style={floatStyle}>
        {/* CEREBRO-PATCH(reply-target-agent-indicator): FIR-2392 — sits
            above the editor card so the user always sees who an untagged
            comment will trigger, matching the reply composer. */}
        <TriggerTargetBar
          markdown={markdown}
          triggerAgentId={triggerAgentId}
          onTagOwner={(owner) => {
            editorRef.current?.insertText(` ${memberMentionMarkdown(owner)} `);
          }}
        />
        <div
          {...dropZoneProps}
          data-testid="comment-input"
          // CEREBRO-PATCH(comment-input-click-to-focus): JEH-1200 — clicks
          // anywhere in the card focus the editor (skip when the target is
          // itself interactive so buttons/links still work).
          onMouseDown={(e) => {
            if ((e.target as HTMLElement).closest("button, a, input, textarea, [contenteditable]")) return;
            e.preventDefault();
            editorRef.current?.focus();
          }}
          // CEREBRO-PATCH(comment-input-min-height): JEH-1065 — minimum 2 lines
          // tall so the placeholder + icon row don't get crammed into one
          // line at the bottom of the issue page (was visibly squashed on
          // mobile — see Jesper's IMG_9779).
          className={cn(
            "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border min-h-20",
            isPinned && "ring-emerald-500/40 shadow-lg",
          )}
        >
          {/* CEREBRO-PATCH(composer-height-cap): TECH-3536 — translucent expand pill, floats above the field on every surface. */}
          {composerHeight.showExpandToggle && (
            <ComposerExpandToggle
              isExpanded={composerHeight.isExpanded}
              onToggle={composerHeight.toggleExpanded}
              expandLabel={t(($) => $.comment.expand_tooltip)}
              collapseLabel={t(($) => $.comment.collapse_tooltip)}
            />
          )}
          {/* CEREBRO-PATCH(composer-height-cap): TECH-3536 — collapsed caps growth, expanded jumps to the larger size. */}
          <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2" style={composerHeight.containerStyle}>
            <ContentEditor
              // CEREBRO-PATCH(input-autofocus): JEH-756 — remount on
              // channel/DM switch so ContentEditor re-evaluates `autoFocus`
              // for the new context (autofocus is read once at create time).
              key={issueId}
              ref={editorRef}
              placeholder={t(($) => $.comment.leave_comment_placeholder)}
              // CEREBRO-PATCH(comment-drafts): TECH-3491 — seed from the stored draft on mount.
              defaultValue={draft.defaultValue}
              onUpdate={(md) => {
                setMarkdown(md);
                setIsEmpty(!md.trim());
                draft.save(md); // CEREBRO-PATCH(comment-drafts): TECH-3491 — persist as you type.
              }}
              onSubmit={handleSubmit}
              onUploadFile={handleUpload}
              debounceMs={100}
              currentIssueId={issueId}
              submitOnEnter={submitOnEnter}
              // CEREBRO-PATCH(input-autofocus): JEH-756 — issue pages stay
              // opt-out (read-first); channel and DM views opt in.
              autoFocus={autoFocus}
            />
          </div>
          {/* CEREBRO-PATCH(file-upload-button-api): paperclip on the left, well
              away from the send button on touch screens, with the new
              onAttach/onEmbed FileUploadButton API. */}
          <div className="absolute bottom-1 left-1.5 flex items-center">
            <FileUploadButton
              size="sm"
              // CEREBRO-PATCH(file-upload-multiple): allow picking multiple files at once
              multiple
              onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
              onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
            />
          </div>
          <div className="absolute bottom-1 right-1.5 flex items-center gap-2 sm:gap-1">
            {/* CEREBRO-PATCH(comment-drafts): TECH-3491 — "Kladde gemt" cue. */}
            <DraftSavedHint show={draft.saved} className="mr-1" />
            {pinEnabled && (
              <PinButton
                isPinned={isPinned}
                onToggle={togglePin}
                pinLabel={t(($) => $.comment.pin_tooltip)}
                unpinLabel={t(($) => $.comment.unpin_tooltip)}
              />
            )}
            <Button
              size="icon-sm"
              aria-label="Submit comment"
              disabled={isEmpty || submitting}
              onClick={handleSubmit}
            >
              {submitting ? (
                <Loader2 className="animate-spin" />
              ) : (
                <ArrowUp />
              )}
            </Button>
          </div>
          {isDragOver && <FileDropOverlay />}
        </div>
      </div>
    </div>
  );
}

export { CommentInput };
