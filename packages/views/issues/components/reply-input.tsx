"use client";

// CEREBRO-PATCH(reply-input-cerebro): cerebro modification of upstream file
// CEREBRO-PATCH(reply-input-pin): JEH-1065 — opt-in pin toggle. While pinned,
// the input behaves as `position: sticky` from the bottom — it stays in its
// natural slot inside the thread until the viewport bottom would scroll past
// it, then it floats with `position: fixed` matching the anchor's width.
// Visual styling stays identical to the unpinned input (no banner, no
// ring/shadow) so the user only sees the green pin icon as state indicator.
// The editor stays in the same React tree slot on every render so a Tiptap
// draft / caret / pending upload survives the pin toggle. Auto-unpins on
// submit and scrolls back to the originating thread.

import { useCallback, useId, useRef, useState } from "react";
import { ArrowUp, Loader2, Maximize2, Minimize2 } from "lucide-react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { useSubmitOnEnter } from "@multica/cerebro-preferences/views";
import { PinButton, useFloatPosition, useInputPin } from "@multica/cerebro-pin-input";
import { useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  issueId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
  size?: "sm" | "default";
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput({
  issueId,
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  size = "default",
}: ReplyInputProps) {
  const { t } = useT("issues");
  const placeholderText = placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  const measureRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const submitOnEnter = useSubmitOnEnter();
  const [isEmpty, setIsEmpty] = useState(true);
  const [isExpanded, setIsExpanded] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const reactId = useId();
  const pinKey = `reply:${issueId}:${reactId}`;
  const { enabled: pinEnabled, isPinned, togglePin, unpin } = useInputPin(pinKey, true);
  const floatRect = useFloatPosition(anchorRef, isPinned, { mode: "sticky-bottom" });
  const isFloating = floatRect !== null;

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

  const avatarSize = size === "sm" ? 22 : 28;

  // Sticky-bottom: stay in normal flow until the viewport scrolls past the
  // anchor; then float with `position: fixed` matching the anchor's width and
  // height. Styling is unchanged so the field looks identical to its
  // not-pinned self — the only state cue is the green pin icon.
  const floatStyle = floatRect
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
      {/* While the input is floating its natural slot is empty; reserve the
          same height with a hidden spacer so the thread layout does not jump
          (which would also break the sticky measurement and trigger a flicker
          loop). */}
      {isFloating && <div aria-hidden style={{ height: floatRect.height }} />}
      <div style={floatStyle}>
        <div className="group/editor flex items-start gap-2.5">
          <ActorAvatar
            actorType={avatarType}
            actorId={avatarId}
            size={avatarSize}
            className="mt-0.5 shrink-0"
          />
          <div
            {...dropZoneProps}
            className={cn(
              "relative min-w-0 flex-1 flex flex-col rounded-md bg-card",
              isExpanded
                ? "h-[60vh]"
                : size === "sm" ? "max-h-40" : "max-h-56",
              (!isEmpty || isExpanded) && "pb-7",
            )}
          >
            <div className="flex-1 min-h-0 overflow-y-auto pr-14 pl-8 sm:pl-0">
              <div ref={measureRef}>
                <ContentEditor
                  ref={editorRef}
                  placeholder={placeholderText}
                  onUpdate={(md) => setIsEmpty(!md.trim())}
                  onSubmit={handleSubmit}
                  onUploadFile={handleUpload}
                  debounceMs={100}
                  currentIssueId={issueId}
                  submitOnEnter={submitOnEnter}
                />
              </div>
            </div>
            {/* CEREBRO-PATCH(reply-input-mobile-paperclip): JEH-1065 — on
                mobile, mirror CommentInput by moving the paperclip to the left
                side, so pin + expand + send aren't cramped together on touch
                screens. Desktop keeps the original right-aligned grouping. */}
            <div className="absolute bottom-0 left-0 flex items-center sm:hidden">
              <FileUploadButton
                size="sm"
                onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
                onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
              />
            </div>
            <div className="absolute bottom-0 right-0 flex items-center gap-4 sm:gap-1">
              {pinEnabled && (
                <PinButton
                  isPinned={isPinned}
                  onToggle={togglePin}
                  pinLabel={t(($) => $.reply.pin_tooltip)}
                  unpinLabel={t(($) => $.reply.unpin_tooltip)}
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
                      className="inline-flex h-6 w-6 items-center justify-center rounded-sm text-muted-foreground opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                    >
                      {isExpanded ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
                    </button>
                  }
                />
                <TooltipContent side="top">{isExpanded ? t(($) => $.reply.collapse_tooltip) : t(($) => $.reply.expand_tooltip)}</TooltipContent>
              </Tooltip>
              <div className="hidden sm:inline-flex">
                <FileUploadButton
                  size="sm"
                  onAttach={(files) => files.forEach((f) => editorRef.current?.uploadFile(f))}
                  onEmbed={(files) => files.forEach((f) => editorRef.current?.uploadFile(f, { embedImage: true }))}
                />
              </div>
              <button
                type="button"
                disabled={isEmpty || submitting}
                onClick={handleSubmit}
                aria-label="Send"
                className="inline-flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-full bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:bg-muted disabled:text-muted-foreground disabled:pointer-events-none"
              >
                {submitting ? (
                  <Loader2 className="h-4 w-4 sm:h-3.5 sm:w-3.5 animate-spin" />
                ) : (
                  <ArrowUp className="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                )}
              </button>
            </div>
            {isDragOver && <FileDropOverlay />}
          </div>
        </div>
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
