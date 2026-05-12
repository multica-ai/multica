"use client";

import { useRef, useState, useCallback, useEffect, useMemo } from "react";
import { ArrowUp, Loader2, Maximize2, Minimize2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";
import { useAuthStore } from "@multica/core/auth";
import { toast } from "sonner";

interface CommentInputProps {
  issueId: string;
  onSubmit: (content: string, attachmentIds?: string[]) => Promise<void>;
}

const COMMENT_DRAFT_STORAGE_PREFIX = "multica:issue-comment-draft";
const COMMENT_DRAFT_VERSION = 1;
const DRAFT_SAVE_DEBOUNCE_MS = 500;

interface CommentDraftUpload {
  url: string;
  id: string;
}

interface CommentDraftPayload {
  version: number;
  issueId: string;
  userId: string;
  content: string;
  uploads: CommentDraftUpload[];
  updatedAt: number;
  tabId: string;
}

function makeDraftKey(issueId: string, userId: string) {
  return `${COMMENT_DRAFT_STORAGE_PREFIX}:${userId}:${issueId}`;
}

function createTabId() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function parseDraft(value: string | null): CommentDraftPayload | null {
  if (!value) return null;
  try {
    const parsed = JSON.parse(value) as Partial<CommentDraftPayload>;
    if (
      parsed.version !== COMMENT_DRAFT_VERSION ||
      typeof parsed.issueId !== "string" ||
      typeof parsed.userId !== "string" ||
      typeof parsed.content !== "string" ||
      typeof parsed.updatedAt !== "number" ||
      typeof parsed.tabId !== "string"
    ) {
      return null;
    }
    return {
      version: COMMENT_DRAFT_VERSION,
      issueId: parsed.issueId,
      userId: parsed.userId,
      content: parsed.content,
      uploads: Array.isArray(parsed.uploads)
        ? parsed.uploads.filter(
            (upload): upload is CommentDraftUpload =>
              typeof upload?.url === "string" && typeof upload?.id === "string",
          )
        : [],
      updatedAt: parsed.updatedAt,
      tabId: parsed.tabId,
    };
  } catch {
    return null;
  }
}

function readDraft(key: string): CommentDraftPayload | null {
  try {
    return parseDraft(window.localStorage.getItem(key));
  } catch {
    return null;
  }
}

function writeDraft(key: string, draft: CommentDraftPayload) {
  try {
    window.localStorage.setItem(key, JSON.stringify(draft));
  } catch {
    // Ignore quota/private-mode failures; the editor must keep working.
  }
}

function removeDraft(key: string, expectedUpdatedAt?: number) {
  try {
    if (expectedUpdatedAt != null) {
      const current = readDraft(key);
      if (current && current.updatedAt !== expectedUpdatedAt) return;
    }
    window.localStorage.removeItem(key);
  } catch {
    // Ignore storage failures.
  }
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const { t } = useT("issues");
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const [restoreDialogOpen, setRestoreDialogOpen] = useState(false);
  const [pendingDraft, setPendingDraft] = useState<CommentDraftPayload | null>(null);
  const uploadMapRef = useRef<Map<string, string>>(new Map());
  const currentContentRef = useRef("");
  const hasUserEditedRef = useRef(false);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const tabIdRef = useRef(createTabId());
  const userId = useAuthStore((s) => s.user?.id);
  const draftKey = useMemo(
    () => (userId ? makeDraftKey(issueId, userId) : null),
    [issueId, userId],
  );
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const clearPendingSave = useCallback(() => {
    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
      saveTimerRef.current = undefined;
    }
  }, []);

  const persistDraft = useCallback((content: string, removeWhenEmpty: boolean) => {
    if (!draftKey || !userId) return;
    if (!content.trim()) {
      if (removeWhenEmpty) removeDraft(draftKey);
      return;
    }

    writeDraft(draftKey, {
      version: COMMENT_DRAFT_VERSION,
      issueId,
      userId,
      content,
      uploads: Array.from(uploadMapRef.current, ([url, id]) => ({ url, id })),
      updatedAt: Date.now(),
      tabId: tabIdRef.current,
    });
  }, [draftKey, issueId, userId]);

  const scheduleDraftSave = useCallback((content: string) => {
    clearPendingSave();
    saveTimerRef.current = setTimeout(() => {
      saveTimerRef.current = undefined;
      persistDraft(content, true);
    }, DRAFT_SAVE_DEBOUNCE_MS);
  }, [clearPendingSave, persistDraft]);

  const flushDraft = useCallback(() => {
    clearPendingSave();
    persistDraft(currentContentRef.current, hasUserEditedRef.current);
  }, [clearPendingSave, persistDraft]);

  useEffect(() => {
    return () => {
      flushDraft();
    };
  }, [flushDraft]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    window.addEventListener("pagehide", flushDraft);
    return () => window.removeEventListener("pagehide", flushDraft);
  }, [flushDraft]);

  useEffect(() => {
    if (!draftKey) return;
    const draft = readDraft(draftKey);
    if (!draft?.content.trim()) return;
    if (draft.content === currentContentRef.current) return;
    setPendingDraft(draft);
    setRestoreDialogOpen(true);
  }, [draftKey]);

  useEffect(() => {
    if (!draftKey || typeof window === "undefined") return;

    const handleStorage = (event: StorageEvent) => {
      if (event.key !== draftKey) return;

      if (!event.newValue) {
        const oldDraft = parseDraft(event.oldValue);
        if (oldDraft?.content === currentContentRef.current) {
          clearPendingSave();
          hasUserEditedRef.current = false;
        }
        setPendingDraft(null);
        setRestoreDialogOpen(false);
        return;
      }

      const draft = parseDraft(event.newValue);
      if (!draft || draft.tabId === tabIdRef.current) return;
      if (!draft.content.trim() || draft.content === currentContentRef.current) return;
      setPendingDraft(draft);
      setRestoreDialogOpen(true);
    };

    window.addEventListener("storage", handleStorage);
    return () => window.removeEventListener("storage", handleStorage);
  }, [clearPendingSave, draftKey]);

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
      clearPendingSave();
      if (draftKey) removeDraft(draftKey);
      currentContentRef.current = "";
      hasUserEditedRef.current = false;
      editorRef.current?.clearContent();
      setIsEmpty(true);
      uploadMapRef.current.clear();
    } catch {
      // The timeline hook already shows the error toast. Keep the draft intact.
    } finally {
      setSubmitting(false);
    }
  };

  const handleEditorUpdate = useCallback((md: string) => {
    currentContentRef.current = md;
    hasUserEditedRef.current = true;
    setIsEmpty(!md.trim());
    scheduleDraftSave(md);
  }, [scheduleDraftSave]);

  const handleRestoreDraft = useCallback(() => {
    if (!pendingDraft) return;
    clearPendingSave();
    currentContentRef.current = pendingDraft.content;
    hasUserEditedRef.current = true;
    uploadMapRef.current = new Map(
      pendingDraft.uploads.map((upload) => [upload.url, upload.id]),
    );
    editorRef.current?.setMarkdown(pendingDraft.content);
    setIsEmpty(!pendingDraft.content.trim());
    setPendingDraft(null);
    setRestoreDialogOpen(false);
  }, [clearPendingSave, pendingDraft]);

  const handleDiscardDraft = useCallback(() => {
    if (draftKey && pendingDraft) {
      removeDraft(draftKey, pendingDraft.updatedAt);
    }
    setPendingDraft(null);
    setRestoreDialogOpen(false);
  }, [draftKey, pendingDraft]);

  return (
    <>
      <div
        {...dropZoneProps}
        className={cn(
          "relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border",
          isExpanded ? "h-[70vh]" : "max-h-56",
        )}
      >
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            ref={editorRef}
            placeholder={t(($) => $.comment.leave_comment_placeholder)}
            onUpdate={handleEditorUpdate}
            onSubmit={handleSubmit}
            onUploadFile={handleUpload}
            debounceMs={100}
            currentIssueId={issueId}
          />
        </div>
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
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
          <FileUploadButton
            size="sm"
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
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
      <AlertDialog open={restoreDialogOpen} onOpenChange={setRestoreDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Restore draft?</AlertDialogTitle>
            <AlertDialogDescription>
              An unsent comment draft exists for this issue. Restoring it will replace the
              current comment editor content.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={handleDiscardDraft}>Discard</AlertDialogCancel>
            <AlertDialogAction onClick={handleRestoreDraft}>Restore</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

export { CommentInput };
