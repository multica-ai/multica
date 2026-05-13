"use client";

// CEREBRO-PATCH(comment-input-cerebro): cerebro modification of upstream file
// CEREBRO-PATCH(comment-input-pin): JEH-1065 — opt-in `pinnable` prop. When
// set (issue pages do, channels/DMs stay opt-out), the input gains a pin
// toggle that pins the field to the bottom of the viewport via inline
// `position: fixed`. The editor stays in the same React tree slot on every
// render — only the wrapper style differs — so a Tiptap draft / caret /
// pending upload survives the pin toggle. Auto-unpins on submit.

import { useCallback, useId, useRef, useState } from "react";
import { ArrowUp, Loader2, Maximize2, Minimize2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { PinButton, useFloatPosition, useInputPin } from "@multica/cerebro-pin-input";
import { useT } from "../../i18n";

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
}

function CommentInput({ issueId, onSubmit, autoFocus = false, pinnable = false }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
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
    // Only send attachment IDs for uploads still present in the content.
    const activeIds: string[] = [];
    for (const [url, id] of uploadMapRef.current) {
      if (content.includes(url)) activeIds.push(id);
    }
    setSubmitting(true);
    try {
      await onSubmit(content, activeIds.length > 0 ? activeIds : undefined);
      editorRef.current?.clearContent();
      setIsEmpty(true);
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
        <div
          {...dropZoneProps}
          data-testid="comment-input"
          // CEREBRO-PATCH(comment-input-min-height): JEH-1065 — minimum 2 lines
          // tall so the placeholder + icon row don't get crammed into one
          // line at the bottom of the issue page (was visibly squashed on
          // mobile — see Jesper's IMG_9779).
          className={cn(
            "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border min-h-20",
            isPinned && "ring-emerald-500/40 shadow-lg",
            isExpanded ? "h-[70vh]" : "max-h-56",
          )}
        >
          <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
            <ContentEditor
              // CEREBRO-PATCH(input-autofocus): JEH-756 — remount on
              // channel/DM switch so ContentEditor re-evaluates `autoFocus`
              // for the new context (autofocus is read once at create time).
              key={issueId}
              ref={editorRef}
              placeholder={t(($) => $.comment.leave_comment_placeholder)}
              onUpdate={(md) => setIsEmpty(!md.trim())}
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
              onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
              onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
            />
          </div>
          <div className="absolute bottom-1 right-1.5 flex items-center gap-2 sm:gap-1">
            {pinEnabled && (
              <PinButton
                isPinned={isPinned}
                onToggle={togglePin}
                pinLabel={t(($) => $.comment.pin_tooltip)}
                unpinLabel={t(($) => $.comment.unpin_tooltip)}
              />
            )}
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={() => {
                      setIsExpanded((v) => !v);
                      editorRef.current?.focus();
                    }}
                    className="rounded-sm p-1.5 text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="top">{isExpanded ? t(($) => $.comment.collapse_tooltip) : t(($) => $.comment.expand_tooltip)}</TooltipContent>
            </Tooltip>
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
