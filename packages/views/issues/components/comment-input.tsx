"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => Promise<void>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  // Read the persisted draft once on mount. ContentEditor only honors
  // `defaultValue` at mount time, so this snapshot drives both the editor's
  // initial content and the submit-button enable state — without this the
  // button would be disabled even though the editor visibly contains text.
  const draftKey = `new:${issueId}` as const;
  const initialDraft = useCommentDraftStore.getState().getDraft(draftKey);
  const [content, setContent] = useState(initialDraft ?? "");
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [submitting, setSubmitting] = useState(false);
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, content });
  // Attachments uploaded in this composer session. Drives both:
  //  - submit-time `attachment_ids` payload (filtered to URLs still in markdown)
  //  - the editor's AttachmentDownloadProvider, so file-card Eye buttons can
  //    resolve text/code/markdown previews that require the attachment id.
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  // Draft persistence. Hydrate from store on mount via `defaultValue` above
  // (ContentEditorRef has no setContent, so this is the only injection point).
  // Flush on every onUpdate (debounced upstream) + visibilitychange/pagehide
  // so tab close / mobile background doesn't lose work. Cleared on submit.
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const clearDraft = useCommentDraftStore((s) => s.clearDraft);
  useEffect(() => {
    const flush = () => {
      const md = editorRef.current?.getMarkdown();
      if (md && md.trim().length > 0) setDraft(draftKey, md);
    };
    const onVis = () => { if (document.visibilityState === "hidden") flush(); };
    document.addEventListener("visibilitychange", onVis);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("pagehide", flush);
    };
  }, [draftKey, setDraft]);

  const handleUpload = useCallback(async (file: File) => {
    const result = await uploadWithToast(file, { issueId });
    if (result) {
      setPendingAttachments((prev) => [...prev, result]);
    }
    return result;
  }, [uploadWithToast, issueId]);

  useEffect(() => {
    setSuppressedAgentIds(new Set());
  }, [issueId]);

  useEffect(() => {
    const visible = new Set(triggerPreview.agents.map((agent) => agent.id));
    setSuppressedAgentIds((prev) => {
      const next = new Set([...prev].filter((id) => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [triggerPreview.agents]);

  const toggleSuppressedAgent = useCallback((agentId: string) => {
    setSuppressedAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  }, []);

  const handleSubmit = async () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || submitting) return;
    // Track every attachment whose stable download URL OR legacy
    // storage URL is referenced in the markdown body. Both shapes
    // can appear in the same comment during the MUL-3130 rollout —
    // see contentReferencesAttachment for the rationale.
    const activeIds = pendingAttachments
      .filter((a) => contentReferencesAttachment(content, a))
      .map((a) => a.id);
    const suppressAgentIds = triggerPreview.agents
      .filter((agent) => suppressedAgentIds.has(agent.id))
      .map((agent) => agent.id);
    setSubmitting(true);
    try {
      await onSubmit(
        content,
        activeIds.length > 0 ? activeIds : undefined,
        suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
      );
      editorRef.current?.clearContent();
      setContent("");
      setIsEmpty(true);
      setSuppressedAgentIds(new Set());
      setPendingAttachments([]);
      clearDraft(draftKey);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      {...dropZoneProps}
      className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border"
    >
      <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
        <ContentEditor
          ref={editorRef}
          defaultValue={initialDraft}
          placeholder={t(($) => $.comment.leave_comment_placeholder)}
          onUpdate={(md) => {
            setContent(md);
            setIsEmpty(!md.trim());
            // Debounced upstream (debounceMs=100). Persist on every tick so a
            // reload or scroll-out-of-viewport restores work to the keystroke.
            if (md.trim().length > 0) setDraft(draftKey, md);
            else clearDraft(draftKey);
          }}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          debounceMs={100}
          currentIssueId={issueId}
          attachments={pendingAttachments}
          enableSlashCommands
          slashCommandMode="command"
        />
      </div>
      <div className="absolute bottom-1 left-2 right-28 min-w-0">
        <CommentTriggerChips
          agents={triggerPreview.agents}
          suppressedAgentIds={suppressedAgentIds}
          onToggle={toggleSuppressedAgent}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton
          size="sm"
          multiple
          onSelect={(file) => editorRef.current?.uploadFile(file)}
        />
        <SubmitButton
          onClick={handleSubmit}
          disabled={isEmpty}
          loading={submitting}
          tooltip={`${t(($) => $.comment.send_tooltip)} · ${formatShortcut(modKey, enterKey)}`}
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
