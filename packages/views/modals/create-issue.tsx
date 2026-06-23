"use client";

import { useState, useRef, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigation } from "../navigation";
import {
  AlertTriangle,
  ArrowDown,
  ArrowLeftRight,
  ArrowUp,
  CalendarClock,
  Check,
  ChevronRight,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  X as XIcon,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Issue, IssueStatus, IssuePriority, IssueAssigneeType, Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types";
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
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { ContentEditor, type ContentEditorRef, TitleEditor, useFileDropZone, FileDropOverlay } from "../editor";
import { StatusIcon, StatusPicker, PriorityPicker, StagePicker, AssigneePicker, StartDatePicker, DueDatePicker } from "../issues/components";
import { maxSiblingStage } from "../issues/components/pickers/stage-picker";
import { ProjectPicker } from "../projects/components/project-picker";
import { useIssueTriggerPreview } from "../issues/hooks/use-issue-trigger-preview";
import { useActorName } from "@multica/core/workspace/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { useCreateModeStore } from "@multica/core/issues/stores/create-mode-store";
import { useQuickCreateStore } from "@multica/core/issues/stores/quick-create-store";
import { issueDetailOptions, childIssuesOptions } from "@multica/core/issues/queries";
import { useCreateIssue, useUpdateIssue } from "@multica/core/issues/mutations";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import {
  api,
  ApiError,
  DuplicateIssueErrorBodySchema,
  type DuplicateIssueErrorBody,
  parseWithFallback,
} from "@multica/core/api";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { PillButton } from "../common/pill-button";
import { ActorAvatar } from "../common/actor-avatar";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

function toDraftAttachment(attachment: Attachment): Attachment {
  return {
    ...attachment,
    // `download_url` is minted for the current API response and may be a
    // short-lived signed URL. Drafts survive across dialog closes and app
    // restarts, so persist only durable fields and let render/download paths
    // re-resolve through id/markdown_url when needed.
    download_url: "",
  };
}

// ---------------------------------------------------------------------------
// ManualCreatePanel — manual-mode body of the create-issue dialog. Renders
// DialogContent + everything inside; the surrounding `<Dialog>` is owned by
// CreateIssueDialog so mode switching swaps only the inner panel without
// remounting the Dialog Root (no overlay flash). `onSwitchMode` flips the
// shell's local mode state.
// ---------------------------------------------------------------------------

// CreateRunHint is the create modal's passive pre-trigger label (MUL-3375 §4):
// whether saving will start a run, driven by the unified backend predicate
// (preview, isCreate) — never a frontend guess. No dialog, no blocking.
//
// Visually it borrows the comment header's avatar+text line, minus the
// interactivity — purely a caption, never a link/hover-card. It renders its own
// reveal band (a grid 0fr→1fr collapse) so it sits on a dedicated row above the
// property toolbar without reflowing anything: collapsed it is 0px (the flex-1
// editor absorbs the delta), and it expands only once the predicate resolves,
// animating straight to the correct copy.
function CreateRunHint({
  assigneeType,
  assigneeId,
  status,
}: {
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  status: IssueStatus;
}) {
  const { t } = useT("modals");
  const { getActorName } = useActorName();
  const isAgentLike = assigneeType === "agent" || assigneeType === "squad";
  const preview = useIssueTriggerPreview({
    isCreate: true,
    assigneeType: assigneeType ?? null,
    assigneeId: assigneeId ?? null,
    status,
    enabled: isAgentLike && !!assigneeId,
  });

  // Reveal only after the predicate resolves so the band animates to the final
  // copy instead of flashing "parked" before the run preview lands.
  const ready = isAgentLike && !!assigneeId && !preview.isLoading;
  const willStart = preview.totalCount > 0;
  const isSquad = assigneeType === "squad";
  const triggerAgentId = preview.triggers[0]?.agent_id ?? assigneeId;

  // Avatar + copy mirror the flow. A squad doesn't "work" — its leader
  // evaluates and delegates — so the squad path keeps the squad as the subject
  // (avatar + name) and uses the leader-delegates copy. A single agent picks
  // the issue up directly; a parked issue shows whoever it was assigned to.
  let avatarType: string;
  let avatarId: string | undefined;
  let text: string;
  if (!willStart) {
    avatarType = assigneeType ?? "agent";
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_parked);
  } else if (isSquad) {
    avatarType = "squad";
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_will_start_squad, {
      name: getActorName("squad", assigneeId ?? ""),
    });
  } else {
    avatarType = "agent";
    avatarId = triggerAgentId;
    text = t(($) => $.run_confirm.create_will_start, {
      name: getActorName("agent", triggerAgentId ?? assigneeId ?? ""),
    });
  }

  return (
    <div
      className={cn(
        "grid shrink-0 transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none",
        ready ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
      )}
      aria-hidden={!ready}
    >
      <div className="overflow-hidden">
        <div
          aria-live="polite"
          className="flex items-center gap-1.5 px-4 pb-1 pt-0.5 text-[0.6875rem] text-muted-foreground"
        >
          {avatarId && (
            <ActorAvatar
              actorType={avatarType}
              actorId={avatarId}
              size={16}
              profileLink={false}
            />
          )}
          <span className="truncate">{text}</span>
        </div>
      </div>
    </div>
  );
}

export function ManualCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
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
}) {
  const { t } = useT("modals");
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
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(() => {
    if (data && "assignee_type" in data) {
      return (data.assignee_type as IssueAssigneeType | null) ?? undefined;
    }
    return draft.assigneeType;
  });
  const [assigneeId, setAssigneeId] = useState<string | undefined>(() => {
    if (data && "assignee_id" in data) {
      return (data.assignee_id as string | null) ?? undefined;
    }
    return draft.assigneeId;
  });
  const [startDate, setStartDate] = useState<string | null>(draft.startDate);
  const [dueDate, setDueDate] = useState<string | null>(draft.dueDate);
  const [projectId, setProjectId] = useState<string | undefined>(
    (data?.project_id as string) || undefined,
  );
  const [parentIssueId, setParentIssueId] = useState<string | undefined>(
    (data?.parent_issue_id as string) || undefined,
  );
  // Stage only applies to a sub-issue; kept local (not in the persisted draft)
  // since it's a per-creation choice tied to the chosen parent.
  const [stage, setStage] = useState<number | null>(
    typeof data?.stage === "number" ? (data.stage as number) : null,
  );
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  // Start date is a low-frequency field — by default it lives in the
  // overflow ⋯ menu. Clicking the menu item flips this open, which both
  // mounts the inline pill (the popover's anchor) AND opens the calendar.
  // When the popover closes without a value set, the pill unmounts again.
  const [startDatePickerOpen, setStartDatePickerOpen] = useState(false);
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
  // Sibling stages under the chosen parent, so the Stage picker can offer the
  // already-used max stage (and one beyond) instead of flooring at Stage 1–3.
  const { data: parentChildren = [] } = useQuery({
    ...childIssuesOptions(wsId, parentIssueId ?? ""),
    enabled: !!parentIssueId,
  });

  const draftAttachments = draft.attachments ?? [];

  // Prune draft attachments whose markdown reference was deleted in an
  // earlier editing session. Runs once on mount: at that point the persisted
  // description IS the draft body (no editor edits have happened yet), so
  // dropping unreferenced records is safe. Don't prune on description updates
  // — an onUpdate flush can race a just-finished upload whose markdown link
  // hasn't been inserted yet, and pruning there would drop a live attachment.
  useEffect(() => {
    const { draft: current } = useIssueDraftStore.getState();
    const attachments = current.attachments ?? [];
    const kept = attachments.filter((a) =>
      contentReferencesAttachment(current.description, a),
    );
    if (kept.length !== attachments.length) setDraft({ attachments: kept });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { uploadWithToast } = useFileUpload(api);
  const handleUpload = async (file: File) => {
    const result = await uploadWithToast(file);
    if (result) {
      const currentAttachments =
        useIssueDraftStore.getState().draft.attachments ?? [];
      const attachments = currentAttachments.some((a) => a.id === result.id)
        ? currentAttachments
        : [...currentAttachments, toDraftAttachment(result)];
      setDraft({ attachments });
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
  const updateStartDate = (v: string | null) => { setStartDate(v); setDraft({ startDate: v }); };
  const updateDueDate = (v: string | null) => { setDueDate(v); setDraft({ dueDate: v }); };

  const createIssueMutation = useCreateIssue();
  const updateIssueMutation = useUpdateIssue();
  const resetForNextIssue = () => {
    setTitle("");
    setStatus("todo");
    setPriority("none");
    setStartDate(null);
    setDueDate(null);
    setProjectId(undefined);
    setParentIssueId(undefined);
    setStage(null);
    setChildIssues([]);
    setDraft({
      title: "",
      description: "",
      status: "todo",
      priority: "none",
      assigneeType,
      assigneeId,
      startDate: null,
      dueDate: null,
      attachments: [],
    });
    descEditorRef.current?.clearContent();
    setFormResetKey((key) => key + 1);
  };

  const handleSubmit = async () => {
    if (!title.trim() || submitting) return;
    setSubmitting(true);
    try {
      const description = descEditorRef.current?.getMarkdown()?.trim() || undefined;
      const activeAttachmentIds = draftAttachments
        .filter((a) => contentReferencesAttachment(description ?? "", a))
        .map((a) => a.id);
      const issue = await createIssueMutation.mutateAsync({
        title: title.trim(),
        description,
        status,
        priority,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        start_date: startDate || undefined,
        due_date: dueDate || undefined,
        attachment_ids: activeAttachmentIds.length > 0 ? activeAttachmentIds : undefined,
        parent_issue_id: parentIssueId,
        // Stage is only meaningful for a sub-issue (relative to its siblings).
        stage: parentIssueId && stage != null ? stage : undefined,
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
        // Aggregate fan-out: N independent requests can fail for N different
        // reasons. The user-facing toast stays count-based (any single
        // err.message would mislead), but log each rejection so developers
        // still have signal in dev-tools / Sentry.
        for (const result of results) {
          if (result.status === "rejected") {
            console.error("[create-issue] sub-issue link failed", result.reason);
          }
        }
        const failed = results.filter((r) => r.status === "rejected").length;
        if (failed > 0) {
          toast.error(
            failed === childIssues.length
              ? t(($) => $.create_issue.toast_link_subissues_all_failed)
              : t(($) => $.create_issue.toast_link_subissues_partial, {
                  failed,
                  total: childIssues.length,
                }),
          );
        }
      }

      setLastAssignee(assigneeType, assigneeId);
      setLastMode("manual");
      clearDraft();
      // The old post-create "agent paused in Backlog" blocking panel is gone —
      // a passive inline hint now warns before submit (MUL-3375). Just close or
      // reset and confirm the create.
      if (keepOpen) {
        resetForNextIssue();
      } else {
        onClose();
      }

      {
        toast.custom((toastId) => (
          <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
            <div className="flex items-center gap-2 mb-2">
              <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
                <Check className="size-3" />
              </div>
              <span className="text-sm font-medium">{t(($) => $.create_issue.toast_created)}</span>
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
                toast.dismiss(toastId);
              }}
            >
              {t(($) => $.create_issue.view_issue)}
            </button>
          </div>
        ), { duration: 5000 });
      }
    } catch (err) {
      // Duplicate-issue is the only structured 409 the create endpoint
      // returns. We schema-guard the body (ApiError.body is `unknown`) so a
      // future server-side rename / drop of `code` / `issue` degrades to the
      // normal error toast instead of throwing inside the toast renderer.
      if (err instanceof ApiError && err.status === 409) {
        const dup = parseWithFallback<DuplicateIssueErrorBody | null>(
          err.body,
          DuplicateIssueErrorBodySchema,
          null,
          { endpoint: "POST /api/workspaces/:wsId/issues (active_duplicate_issue)" },
        );
        if (dup) {
          toast.custom(
            (toastId) => (
              <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
                <div className="flex items-center gap-2 mb-2">
                  <div className="flex items-center justify-center size-5 rounded-full bg-amber-500/15 text-amber-500">
                    <AlertTriangle className="size-3" />
                  </div>
                  <span className="text-sm font-medium">
                    {t(($) => $.create_issue.toast_duplicate_title)}
                  </span>
                </div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
                  <span className="truncate">{dup.issue.identifier} – {dup.issue.title}</span>
                </div>
                <button
                  type="button"
                  className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
                  onClick={() => {
                    router.push(p.issueDetail(dup.issue.id));
                    toast.dismiss(toastId);
                  }}
                >
                  {t(($) => $.create_issue.toast_duplicate_view)}
                </button>
              </div>
            ),
            { duration: 5000 },
          );
          return;
        }
      }
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.create_issue.toast_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  // Switch to agent mode. Hand the typed text up to the shell as the carry
  // payload; the shell stores it as the next panel's `data` so the agent
  // panel reads `data.prompt` on mount. Concatenate title + description so
  // nothing the user typed is lost — the agent derives a fresh title from
  // the combined text. Persist the mode flip so the next `c` lands in agent.
  // Also forward the picked project so the agent panel pins the new issue
  // to it; without this the agent panel would fall back to its persisted
  // `lastProjectId`, silently routing the issue to the wrong project.
  // Forward squad picks alongside agent picks so the agent panel honors
  // the actor the user already chose — otherwise a squad selection silently
  // falls back to the persisted actor / first visible agent on flip.
  // parent_issue_id rides through the same carry channel: the modal opener
  // (openCreateSubIssue) seeded it on the manual panel, and the agent panel
  // needs it so the new issue is still created as a sub-issue when the user
  // flips from "Add sub issue" → "Create with agent".
  const switchToAgent = () => {
    const desc = descEditorRef.current?.getMarkdown()?.trim() ?? "";
    const prompt = [title.trim(), desc].filter(Boolean).join("\n\n");
    // Title + description have been packed into the agent prompt — clear them
    // from the shared draft so a later agent→manual switch doesn't surface
    // stale manual state on top of the prompt-as-description, which would
    // duplicate content on every round-trip.
    setDraft({ title: "", description: "" });
    setLastMode("agent");
    // Prefer the hydrated identifier from `parentIssue`, but fall back to the
    // identifier the modal opener seeded on `data`. Without the fallback, a
    // flip that happens before the issue detail query resolves drops the
    // identifier and the agent chip renders as "Sub-issue of " with an empty
    // tail. The UUID alone still wires the sub-issue relationship correctly;
    // this only affects the display affordance.
    const carryParentIdentifier =
      parentIssue?.identifier ?? (data?.parent_issue_identifier as string | undefined);
    onSwitchMode?.({
      prompt,
      ...(assigneeId && assigneeType === "agent"
        ? { agent_id: assigneeId }
        : assigneeId && assigneeType === "squad"
          ? { squad_id: assigneeId }
          : {}),
      ...(projectId ? { project_id: projectId } : {}),
      ...(parentIssueId ? { parent_issue_id: parentIssueId } : {}),
      ...(carryParentIdentifier ? { parent_issue_identifier: carryParentIdentifier } : {}),
    });
  };

  return (
    <>
            <DialogTitle className="sr-only">{t(($) => $.create_issue.sr_manual)}</DialogTitle>

            {/* Header */}
            <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
              <div className="flex items-center gap-1.5 text-xs">
                <span className="text-muted-foreground">{workspaceName}</span>
                <ChevronRight className="size-3 text-muted-foreground/50" />
                <span className="font-medium">{t(($) => $.create_issue.manual_breadcrumb)}</span>
              </div>
              <div className="flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={() => setIsExpanded(!isExpanded)}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">
                    {isExpanded
                      ? t(($) => $.common.collapse_tooltip)
                      : t(($) => $.common.expand_tooltip)}
                  </TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={onClose}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        <XIcon className="size-4" />
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">{t(($) => $.common.close)}</TooltipContent>
                </Tooltip>
              </div>
            </div>

            {/* Title */}
            <div className="px-5 pb-2 shrink-0">
              <TitleEditor
                key={formResetKey}
                autoFocus
                defaultValue={draft.title}
                placeholder={t(($) => $.create_issue.title_placeholder)}
                className="text-lg font-semibold"
                onChange={(v) => updateTitle(v)}
                onSubmit={handleSubmit}
              />
            </div>

            {/* Description — takes remaining space */}
            <div {...descDropZoneProps} className="relative flex flex-1 min-h-0 overflow-y-auto px-5">
              <ContentEditor
                ref={descEditorRef}
                defaultValue={draft.description}
                placeholder={t(($) => $.create_issue.description_placeholder)}
                onUpdate={(md) => setDraft({ description: md })}
                onUploadFile={handleUpload}
                debounceMs={500}
                attachments={draftAttachments}
              />
              {descDragOver && <FileDropOverlay />}
            </div>

            {/* Pre-trigger preview — a passive caption above the toolbar; reveals
                when an agent assignee will pick the issue up. */}
            <CreateRunHint assigneeType={assigneeType} assigneeId={assigneeId} status={status} />

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

              {/* Stage — only relevant when creating a sub-issue under a parent */}
              {parentIssueId && (
                <StagePicker
                  stage={stage}
                  onUpdate={(u) => setStage(u.stage ?? null)}
                  maxStage={maxSiblingStage(parentChildren)}
                  triggerRender={<PillButton />}
                  align="start"
                />
              )}

              {/* Start date — collapsed into the ⋯ menu by default since it's
                  a low-frequency field. Renders inline only when the field
                  has a value OR the user just opened it from the overflow
                  menu (the picker's calendar popover needs the inline pill
                  as its anchor). */}
              {(startDate || startDatePickerOpen) && (
                <StartDatePicker
                  startDate={startDate}
                  onUpdate={(u) => updateStartDate(u.start_date ?? null)}
                  triggerRender={<PillButton />}
                  align="start"
                  open={startDatePickerOpen}
                  onOpenChange={setStartDatePickerOpen}
                />
              )}

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
                    <span>
                      {t(($) => $.create_issue.subissue_of, { identifier: parentIssue.identifier })}
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setParentIssueId(undefined)}
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label={t(($) => $.create_issue.remove_parent_aria)}
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
                    <span>{t(($) => $.create_issue.subissue_chip, { identifier: c.identifier })}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      setChildIssues((prev) => prev.filter((x) => x.id !== c.id))
                    }
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label={t(($) => $.create_issue.remove_subissue_aria, { identifier: c.identifier })}
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
                    <PillButton aria-label={t(($) => $.create_issue.more_options_aria)}>
                      <MoreHorizontal className="size-3.5" />
                    </PillButton>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto">
                  {!startDate && (
                    <DropdownMenuItem onClick={() => setStartDatePickerOpen(true)}>
                      <CalendarClock className="h-3.5 w-3.5" />
                      {t(($) => $.create_issue.set_start_date)}
                    </DropdownMenuItem>
                  )}
                  {parentIssueId && parentIssue ? (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      {t(($) => $.create_issue.parent_with_id, { identifier: parentIssue.identifier })}
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      {t(($) => $.create_issue.set_parent)}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onClick={() => setChildPickerOpen(true)}>
                    <ArrowDown className="h-3.5 w-3.5" />
                    {t(($) => $.create_issue.add_subissue)}
                  </DropdownMenuItem>
                  {parentIssueId && parentIssue && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setParentIssueId(undefined)}
                      >
                        <XIcon className="h-3.5 w-3.5" />
                        {t(($) => $.create_issue.remove_parent)}
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
              title={t(($) => $.create_issue.set_parent_picker.title)}
              description={t(($) => $.create_issue.set_parent_picker.description)}
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
              title={t(($) => $.create_issue.add_subissue_picker.title)}
              description={t(($) => $.create_issue.add_subissue_picker.description)}
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
                  title={t(($) => $.create_issue.switch_to_agent_tooltip)}
                  className="border-beam group flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground bg-brand/5 hover:bg-brand/10 hover:text-foreground transition-colors cursor-pointer"
                >
                  <ArrowLeftRight className="size-3.5 text-brand/80 transition-transform duration-300 group-hover:rotate-180" />
                  {t(($) => $.create_issue.switch_to_agent)}
                </button>
                <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
                  <Switch
                    size="sm"
                    checked={keepOpen}
                    onCheckedChange={setKeepOpen}
                  />
                  {t(($) => $.create_issue.create_another)}
                </label>
                {!title.trim() ? (
                  <TooltipProvider delay={200}>
                    <Tooltip>
                      <TooltipTrigger render={<span><Button size="sm" onClick={handleSubmit} disabled>{t(($) => $.create_issue.submit)}</Button></span>} />
                      <TooltipContent side="top">{t(($) => $.create_issue.title_required)}</TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                ) : (
                  <Button size="sm" onClick={handleSubmit} disabled={submitting}>
                    {submitting ? t(($) => $.create_issue.submitting) : t(($) => $.create_issue.submit)}
                  </Button>
                )}
              </div>
            </div>
    </>
  );
}

/** className for DialogContent in manual mode — depends on isExpanded.
 *  Exported so the shell (which now owns the DialogContent) can apply the same
 *  visual treatment without duplicating it. */
export function manualDialogContentClass(isExpanded: boolean) {
  return cn(
    "p-0 gap-0 flex flex-col overflow-hidden",
    "!top-1/2 !left-1/2 !-translate-x-1/2",
    "!transition-all !duration-300 !ease-out",
    isExpanded
      ? "!max-w-4xl !w-full !h-5/6 !-translate-y-1/2"
      : "!max-w-2xl !w-full !h-96 !-translate-y-1/2",
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
  return (
    <DialogRoot open onOpenChange={(v) => { if (!v) props.onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={manualDialogContentClass(isExpanded)}
      >
        <ManualCreatePanel
          {...props}
          isExpanded={isExpanded}
          setIsExpanded={setIsExpanded}
        />
      </DialogContent>
    </DialogRoot>
  );
}
