"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
} from "../editor";
import {
  useCreateFeedback,
  useFeedbackDraftStore,
  FEEDBACK_KINDS,
  type FeedbackKind,
} from "@multica/core/feedback";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useT } from "../i18n";
import { useShortcut } from "@multica/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";

const MAX_MESSAGE_LEN = 10000;

const FEEDBACK_KIND_SET = new Set<FeedbackKind>(FEEDBACK_KINDS);

function composeFeedbackInitialMessage(draftMessage: string, incomingInitialMessage: string) {
  const draft = draftMessage.trim();
  const incoming = incomingInitialMessage.trim();
  if (!incoming) return draftMessage;
  if (!draft) return incomingInitialMessage;
  if (draft.includes(incoming)) return draftMessage;
  return `${draftMessage}

---

${incomingInitialMessage}`;
}

export function FeedbackModal({
  onClose,
  data,
  initialMessage,
}: {
  onClose: () => void;
  data?: Record<string, unknown> | null;
  initialMessage?: string;
}) {
  const sendShortcut = useShortcut("send");
  const { t } = useT("modals");
  const workspace = useCurrentWorkspace();
  const draft = useFeedbackDraftStore((s) => s.draft);
  const setDraft = useFeedbackDraftStore((s) => s.setDraft);
  const clearDraft = useFeedbackDraftStore((s) => s.clearDraft);

  const editorRef = useRef<ContentEditorRef>(null);
  const incomingInitialMessage =
    initialMessage ?? (typeof data?.initialMessage === "string" ? data.initialMessage : "");
  const kind = typeof data?.kind === "string" && FEEDBACK_KIND_SET.has(data.kind as FeedbackKind)
    ? (data.kind as FeedbackKind)
    : undefined;
  const seededMessage = composeFeedbackInitialMessage(draft.message, incomingInitialMessage);
  const [message, setMessage] = useState(seededMessage);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });
  const { uploadWithToast } = useFileUpload(api);
  const mutation = useCreateFeedback();

  const canSubmit =
    message.trim().length > 0 &&
    message.length <= MAX_MESSAGE_LEN &&
    !mutation.isPending;

  const handleSubmit = async () => {
    // The button can use debounced `message` state, but the keyboard shortcut
    // must not: Command+Enter can arrive before ContentEditor's 150ms onUpdate
    // fires. The editor ref below is the submit-time source of truth.
    if (mutation.isPending) return;
    if (editorRef.current?.hasActiveUploads()) {
      toast.info(t(($) => $.feedback.toast_uploading));
      return;
    }
    // Read from the editor ref at submit time — `message` state lags 150ms
    // behind keystrokes due to `debounceMs`, so ⌘+Enter fired immediately
    // after typing would otherwise submit stale content.
    const latest = editorRef.current?.getMarkdown()?.trim() ?? "";
    if (!latest) return;
    if (latest.length > MAX_MESSAGE_LEN) {
      toast.error(t(($) => $.feedback.toast_too_long));
      return;
    }
    try {
      await mutation.mutateAsync({
        message: latest,
        url: typeof window !== "undefined" ? window.location.href : undefined,
        workspace_id: workspace?.id,
        kind,
      });
      clearDraft();
      toast.success(t(($) => $.feedback.toast_sent));
      onClose();
    } catch (err) {
      const msg =
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.feedback.toast_failed);
      toast.error(msg);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-2xl !h-[28rem] p-0 gap-0 flex flex-col overflow-hidden">
        <DialogHeader className="px-5 pt-4 pb-2 shrink-0">
          <DialogTitle>{t(($) => $.feedback.title)}</DialogTitle>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.feedback.github_hint_prefix)}
            <a
              href="https://github.com/multica-ai/multica/issues"
              target="_blank"
              rel="noopener noreferrer"
              className="text-brand underline decoration-brand/40 underline-offset-2 hover:decoration-brand"
            >
              {t(($) => $.feedback.github_hint_link)}
            </a>
          </p>
        </DialogHeader>

        <div className="flex-1 min-h-0 px-5 pb-3">
          <div
            {...dropZoneProps}
            className="relative h-full overflow-y-auto rounded-lg border-1 border-border transition-colors focus-within:border-brand"
          >
            <ContentEditor
              ref={editorRef}
              defaultValue={seededMessage}
              placeholder={t(($) => $.feedback.placeholder)}
              onUpdate={(md) => { setMessage(md); setDraft({ message: md }); }}
              onUploadFile={uploadWithToast}
              onSubmit={handleSubmit}
              debounceMs={150}
              showBubbleMenu={false}
              className="px-3 py-2"
            />
            {isDragOver && <FileDropOverlay />}
          </div>
        </div>

        <div className="flex items-center justify-between px-4 py-3 border-t shrink-0">
          <FileUploadButton
            size="sm"
            multiple
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
          <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
            {mutation.isPending ? t(($) => $.feedback.sending) : t(($) => $.feedback.send)}
            {sendShortcut ? (
              <ShortcutKeycaps
                shortcut={sendShortcut}
                decorative
                className="ml-1"
                keyClassName="border-background/30 bg-background/15 text-primary-foreground shadow-none"
              />
            ) : null}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
