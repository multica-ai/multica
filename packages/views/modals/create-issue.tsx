"use client";

import { useState, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigation } from "../navigation";
import {
  ArrowDown,
  ArrowLeftRight,
  ArrowUp,
  Check,
  ChevronRight,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  X as XIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Issue, IssueStatus, IssuePriority, IssueAssigneeType } from "@multica/core/types";
import {
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { ContentEditor, type ContentEditorRef, TitleEditor, useFileDropZone, FileDropOverlay } from "../editor";
import { StatusIcon, StatusPicker, PriorityPicker, AssigneePicker, DueDatePicker } from "../issues/components";
import { BacklogAgentHintContent } from "../issues/components/backlog-agent-hint-dialog";
import { ProjectPicker } from "../projects/components/project-picker";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useCreateModeStore } from "@multica/core/issues/stores/create-mode-store";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCreateIssue, useUpdateIssue } from "@multica/core/issues/mutations";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { PillButton } from "../common/pill-button";
import { IssuePickerModal } from "./issue-picker-modal";

// ---------------------------------------------------------------------------
// ManualCreatePanel — manual-mode body of the create-issue dialog. Renders
// DialogContent + everything inside; the surrounding `<Dialog>` is owned by
// CreateIssueDialog so mode switching swaps only the inner panel without
// remounting the Dialog Root (no overlay flash). `onSwitchMode` flips the
// shell's local mode state.
// ---------------------------------------------------------------------------

export function ManualCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
  backlogHintIssueId,
  setBacklogHintIssueId,
}: {
  onClose: () => void;
  /** Called with the carry payload to seed the agent panel after switch. */
  onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
  /** Lifted to the shell so DialogContent's mode-aware className can react
   *  without the body itself having to live inside DialogContent (which would
   *  re-mount the Portal on mode swap and replay the open animation). */
  isExpanded: boolean;
  setIsExpanded: (v: boolean) => void;
  backlogHintIssueId: string | null;
  setBacklogHintIssueId: (id: string | null) => void;
}) {
  const router = useNavigation();
  const p = useWorkspacePaths();
  const workspaceName = useCurrentWorkspace()?.name;

  const draft = useIssueDraftStore((s) => s.draft);
  const setDraft = useIssueDraftStore((s) => s.setDraft);
  const clearDraft = useIssueDraftStore((s) => s.clearDraft);
  const setLastAssignee = useIssueDraftStore((s) => s.setLastAssignee);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);

  const [title, setTitle] = useState(draft.title);
  const [formResetKey, setFormResetKey] = useState(0);
  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => descEditorRef.current?.uploadFile(f)),
  });
  const [status, setStatus] = useState<IssueStatus>((data?.status as IssueStatus) || draft.status);
  const [priority, setPriority] = useState<IssuePriority>(draft.priority);
  const [submitting, setSubmitting] = useState(false);
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(draft.assigneeType);
  const [assigneeId, setAssigneeId] = useState<string | undefined>(draft.assigneeId);
  const [dueDate, setDueDate] = useState<string | null>(draft.dueDate);
  const [projectId, setProjectId] = useState<string | undefined>(
    (data?.project_id as string) || undefined,
  );
  const [parentIssueId, setParentIssueId] = useState<string | undefined>(
    (data?.parent_issue_id as string) || undefined,
  );
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  // Children live as full Issue objects — the picker always returns the whole
  // object, and we never need to hydrate from an ID the way we do for parent.
  const [childIssues, setChildIssues] = useState<Issue[]>([]);
  const [childPickerOpen, setChildPickerOpen] = useState(false);
  // Fetch parent issue details for the chip (status/identifier/title).
  // List cache usually has it already, so this resolves synchronously.
  const wsId = useWorkspaceId();
  const { data: parentIssue } = useQuery({
    ...issueDetailOptions(wsId, parentIssueId ?? ""),
    enabled: !!parentIssueId,
  });

  // File upload — collect attachment IDs so we can link them after issue creation.
  const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
  const { uploadWithToast } = useFileUpload(api);
  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file);
    if (result) {
      setAttachmentIds((prev) => [...prev, result.id]);
    }
    return result;
  };

  // Sync field changes to draft store
  const updateTitle = (v: string) => { setTitle(v); setDraft({ title: v }); };
  const updateStatus = (v: IssueStatus) => { setStatus(v); setDraft({ status: v }); };
  const updatePriority = (v: IssuePriority) => { setPriority(v); setDraft({ priority: v }); };
  const updateAssignee = (type?: IssueAssigneeType, id?: string) => {
    setAssigneeType(type); setAssigneeId(id);
    setDraft({ assigneeType: type, assigneeId: id });
  };
  const updateDueDate = (v: string | null) => { setDueDate(v); setDraft({ dueDate: v }); };

  const createIssueMutation = useCreateIssue();
  const updateIssueMutation = useUpdateIssue();
  const resetForNextIssue = () => {
    setTitle("");
    setStatus("todo");
    setPriority("none");
    setDueDate(null);
    setProjectId(undefined);
    setParentIssueId(undefined);
    setChildIssues([]);
    setAttachmentIds([]);
    setDraft({
      title: "",
      description: "",
      status: "todo",
      priority: "none",
      assigneeType,
      assigneeId,
      dueDate: null,
    });
    descEditorRef.current?.clearContent();
    setFormResetKey((key) => key + 1);
  };

  const handleSubmit = async () => {
    if (!title.trim() || submitting) return;
    setSubmitting(true);
    try {
      const issue = await createIssueMutation.mutateAsync({
        title: title.trim(),
        description: descEditorRef.current?.getMarkdown()?.trim() || undefined,
        status,
        priority,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        due_date: dueDate || undefined,
        attachment_ids: attachmentIds.length > 0 ? attachmentIds : undefined,
        parent_issue_id: parentIssueId,
        project_id: projectId,
      });

      // Link queued children to the new parent. Deferred to after create
      // because the new issue's ID doesn't exist yet. Partial failures don't
      // roll back the new issue — it's already committed.
      if (childIssues.length > 0) {
        const results = await Promise.allSettled(
          childIssues.map((child) =>
            updateIssueMutation.mutateAsync({
              id: child.id,
              parent_issue_id: issue.id,
            }),
          ),
        );
        const failed = results.filter((r) => r.status === "rejected").length;
        if (failed > 0) {
          toast.error(
            failed === childIssues.length
              ? "Failed to link sub-issues"
              : `Failed to link ${failed} of ${childIssues.length} sub-issues`,
          );
        }
      }

      setLastAssignee(assigneeType, assigneeId);
      setLastMode("manual");
      clearDraft();
      const shouldShowBacklogHint =
        status === "backlog" && assigneeType === "agent" && assigneeId &&
        localStorage.getItem("multica:backlog-agent-hint-dismissed") !== "true";

      if (shouldShowBacklogHint) {
        setBacklogHintIssueId(issue.id);
      } else if (keepOpen) {
        resetForNextIssue();
      } else {
        onClose();
      }

      if (!shouldShowBacklogHint) {
        toast.custom((t) => (
          <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
            <div className="flex items-center gap-2 mb-2">
              <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
                <Check className="size-3" />
              </div>
              <span className="text-sm font-medium">Issue created</span>
            </div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
              <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
              <span className="truncate">{issue.identifier} – {issue.title}</span>
            </div>
            <button
              type="button"
              className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
              onClick={() => {
                router.push(p.issueDetail(issue.id));
                toast.dismiss(t);
              }}
            >
              View issue
            </button>
          </div>
        ), { duration: 5000 });
      }
    } catch {
      toast.error("Failed to create issue");
    } finally {
      setSubmitting(false);
    }
  };

  // Switch to agent mode. Hand the typed text up to the shell as the carry
  // payload; the shell stores it as the next panel's `data` so the agent
  // panel reads `data.prompt` on mount. Concatenate title + description so
  // nothing the user typed is lost — the agent derives a fresh title from
  // the combined text. Persist the mode flip so the next `c` lands in agent.
  const switchToAgent = () => {
    const desc = descEditorRef.current?.getMarkdown()?.trim() ?? "";
    const prompt = [title.trim(), desc].filter(Boolean).join("\n\n");
    setLastMode("agent");
    onSwitchMode?.({
      prompt,
      ...(projectId ? { project_id: projectId } : {}),
      ...(assigneeType === "agent" && assigneeId
        ? { agent_id: assigneeId }
        : {}),
    });
  };

  return (
    <>
        {backlogHintIssueId ? (
          <BacklogAgentHintContent
            onKeepInBacklog={() => {
              setBacklogHintIssueId(null);
              onClose();
            }}
            onDismissPermanently={() => {
              localStorage.setItem("multica:backlog-agent-hint-dismissed", "true");
            }}
            onMoveToTodo={() => {
              updateIssueMutation.mutate(
                { id: backlogHintIssueId, status: "todo" },
                { onError: () => toast.error("Failed to update status") },
              );
              setBacklogHintIssueId(null);
              onClose();
            }}
          />
        ) : (
          <>
            <DialogTitle className="sr-only">New Issue</DialogTitle>

            {/* Header */}
            <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
              <div className="flex items-center gap-1.5 text-xs">
                <span className="text-muted-foreground">{workspaceName}</span>
                <ChevronRight className="size-3 text-muted-foreground/50" />
                <span className="font-medium">Create manually</span>
              </div>
              <div className="flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        onClick={() => setIsExpanded(!isExpanded)}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        onClick={onClose}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        <XIcon className="size-4" />
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">Close</TooltipContent>
                </Tooltip>
              </div>
            </div>

            {/* Title */}
            <div className="px-5 pb-2 shrink-0">
              <TitleEditor
                key={formResetKey}
                autoFocus
                defaultValue={draft.title}
                placeholder="Issue title"
                className="text-lg font-semibold"
                onChange={(v) => updateTitle(v)}
                onSubmit={handleSubmit}
              />
            </div>

            {/* Description — takes remaining space */}
            <div {...descDropZoneProps} className="relative flex-1 min-h-0 overflow-y-auto px-5">
              <ContentEditor
                ref={descEditorRef}
                defaultValue={draft.description}
                placeholder="Add description..."
                onUpdate={(md) => setDraft({ description: md })}
                onUploadFile={handleUpload}
                debounceMs={500}
              />
              {descDragOver && <FileDropOverlay />}
            </div>

            {/* Property toolbar */}
            <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
              {/* Status */}
              <StatusPicker
                status={status}
                onUpdate={(u) => { if (u.status) updateStatus(u.status); }}
                triggerRender={<PillButton />}
                align="start"
              />

              {/* Priority */}
              <PriorityPicker
                priority={priority}
                onUpdate={(u) => { if (u.priority) updatePriority(u.priority); }}
                triggerRender={<PillButton />}
                align="start"
              />

              {/* Assignee */}
              <AssigneePicker
                assigneeType={assigneeType ?? null}
                assigneeId={assigneeId ?? null}
                onUpdate={(u) => updateAssignee(
                  u.assignee_type ?? undefined,
                  u.assignee_id ?? undefined,
                )}
                triggerRender={<PillButton />}
                align="start"
              />

              {/* Due date */}
              <DueDatePicker
                dueDate={dueDate}
                onUpdate={(u) => updateDueDate(u.due_date ?? null)}
                triggerRender={<PillButton />}
                align="start"
              />

              {/* Project */}
              <ProjectPicker
                projectId={projectId ?? null}
                onUpdate={(u) => setProjectId(u.project_id ?? undefined)}
                triggerRender={<PillButton />}
                align="start"
              />

              {/* Parent chip — appears when parent is set.
                  Placed before the ⋯ so it wraps to a new line with ⋯ if
                  space is tight, but ⋯ always stays last in DOM order. */}
              {parentIssueId && parentIssue && (
                <div className="inline-flex items-center rounded-full border text-xs transition-colors hover:bg-accent/60">
                  <button
                    type="button"
                    onClick={() => setParentPickerOpen(true)}
                    className="flex items-center gap-1.5 py-1 pl-2.5 cursor-pointer"
                  >
                    <ArrowUp className="size-3 text-muted-foreground" />
                    <span>Sub-issue of {parentIssue.identifier}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setParentIssueId(undefined)}
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label="Remove parent"
                  >
                    <XIcon className="size-3" />
                  </button>
                </div>
              )}

              {/* Child chips — one per queued sub-issue. Links are deferred
                  until create resolves (see handleSubmit). */}
              {childIssues.map((c) => (
                <div
                  key={c.id}
                  className="inline-flex items-center rounded-full border text-xs transition-colors hover:bg-accent/60"
                >
                  <div className="flex items-center gap-1.5 py-1 pl-2.5">
                    <ArrowDown className="size-3 text-muted-foreground" />
                    <span>Sub-issue: {c.identifier}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      setChildIssues((prev) => prev.filter((x) => x.id !== c.id))
                    }
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label={`Remove sub-issue ${c.identifier}`}
                  >
                    <XIcon className="size-3" />
                  </button>
                </div>
              ))}

              {/* Overflow — always the last child so DOM order keeps it at the
                  end of the wrap flow, no matter how many chips are present. */}
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <PillButton aria-label="More options">
                      <MoreHorizontal className="size-3.5" />
                    </PillButton>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto">
                  {parentIssueId && parentIssue ? (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      Parent: {parentIssue.identifier}
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      Set parent issue...
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onClick={() => setChildPickerOpen(true)}>
                    <ArrowDown className="h-3.5 w-3.5" />
                    Add sub-issue...
                  </DropdownMenuItem>
                  {parentIssueId && parentIssue && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setParentIssueId(undefined)}
                      >
                        <XIcon className="h-3.5 w-3.5" />
                        Remove parent
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>

            {/* Parent / child pickers — rendered inline so they stack over this
                modal instead of replacing it via useModalStore. */}
            <IssuePickerModal
              open={parentPickerOpen}
              onOpenChange={setParentPickerOpen}
              title="Set parent issue"
              description="Search for an issue to set as the parent of the new issue"
              excludeIds={[
                ...childIssues.map((c) => c.id),
                ...(parentIssueId ? [parentIssueId] : []),
              ]}
              onSelect={(selected) => {
                setParentIssueId(selected.id);
              }}
            />
            <IssuePickerModal
              open={childPickerOpen}
              onOpenChange={setChildPickerOpen}
              title="Add sub-issue"
              description="Search for an issue to add as a sub-issue of the new issue"
              excludeIds={[
                ...childIssues.map((c) => c.id),
                ...(parentIssueId ? [parentIssueId] : []),
              ]}
              onSelect={(selected) => {
                setChildIssues((prev) =>
                  prev.some((x) => x.id === selected.id) ? prev : [...prev, selected],
                );
              }}
            />

            {/* Footer */}
            <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-h-7 items-center gap-2">
                <FileUploadButton
                  onSelect={(file) => descEditorRef.current?.uploadFile(file)}
                />
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={switchToAgent}
                  title="Switch to create with agent — describe in one line and let the agent file it"
                  className="flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors cursor-pointer"
                >
                  <ArrowLeftRight className="size-3.5" />
                  Switch to Agent
                </button>
                <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
                  <Switch
                    size="sm"
                    checked={keepOpen}
                    onCheckedChange={setKeepOpen}
                  />
                  Create another
                </label>
                <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || submitting}>
                  {submitting ? "Creating..." : "Create Issue"}
                </Button>
              </div>
            </div>
          </>
        )}
    </>
  );
}

/** className for DialogContent in manual mode — depends on isExpanded and the
 *  backlog-hint sub-state. Exported so the shell (which now owns the
 *  DialogContent) can apply the same visual treatment without duplicating it. */
export function manualDialogContentClass(
  isExpanded: boolean,
  backlogHintIssueId: string | null,
) {
  return cn(
    "p-0 gap-0 flex flex-col overflow-hidden",
    "!top-1/2 !left-1/2 !-translate-x-1/2",
    backlogHintIssueId
      ? "!max-w-[480px] !w-[calc(100vw-2rem)] !h-auto !-translate-y-1/2 !transition-none !duration-0"
      : "!transition-all !duration-300 !ease-out",
    !backlogHintIssueId && isExpanded
      ? "!max-w-4xl !w-full !h-5/6 !-translate-y-1/2"
      : !backlogHintIssueId
        ? "!max-w-2xl !w-full !h-96 !-translate-y-1/2"
        : "",
  );
}

// Thin Dialog-wrapping export — registry mounts the panel directly under the
// shell's shared Dialog, but a few legacy callers (and the test suite) still
// import this module's modal version. Equivalent runtime behavior to the
// pre-refactor component when used standalone.
import { Dialog as DialogRoot } from "@multica/ui/components/ui/dialog";
export function CreateIssueModal(props: {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}) {
  const [isExpanded, setIsExpanded] = useState(false);
  const [backlogHintIssueId, setBacklogHintIssueId] = useState<string | null>(null);
  return (
    <DialogRoot open onOpenChange={(v) => { if (!v) props.onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={manualDialogContentClass(isExpanded, backlogHintIssueId)}
      >
        <ManualCreatePanel
          {...props}
          isExpanded={isExpanded}
          setIsExpanded={setIsExpanded}
          backlogHintIssueId={backlogHintIssueId}
          setBacklogHintIssueId={setBacklogHintIssueId}
        />
      </DialogContent>
    </DialogRoot>
  );
}
