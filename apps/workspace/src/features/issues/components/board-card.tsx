"use client";

import { useCallback, memo, useState } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@/shared/types";
import {
  CalendarDays,
  Calendar,
  Check,
  Copy,
  Link2,
  MoreHorizontal,
  Trash2,
  UserMinus,
} from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useIssueMutations } from "@/features/issues/mutations";
import {
  formatIssueSchedule,
  isIssueScheduleOverdue,
} from "@/features/issues/utils/workbench-view";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { PriorityPicker, AssigneePicker, DueDatePicker, canAssignAgent } from "./pickers";
import {
  ALL_STATUSES,
  STATUS_CONFIG,
  PRIORITY_ORDER,
  PRIORITY_CONFIG,
} from "@/features/issues/config";
import { useViewStore } from "@/features/issues/stores/view-store-context";
import { useWorkspaceStore } from "@/features/workspace";
import { useAuthStore } from "@/features/auth";
import { useModalStore } from "@/features/modals";
import { buildIssueTemplateData } from "@/features/issues/utils/template";
import { Link } from "@/shared/router";
import { IssueTaskStatusBadge } from "./issue-task-status-badge";

/** Stops event from bubbling to Link/drag handlers */
function PickerWrapper({ children }: { children: React.ReactNode }) {
  const stop = (e: React.SyntheticEvent) => {
    e.stopPropagation();
    e.preventDefault();
  };
  return (
    <div onClick={stop} onMouseDown={stop} onPointerDown={stop}>
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Board card action menu (⋯) — mirrors the detail page more-actions dropdown
// ---------------------------------------------------------------------------

function BoardCardActions({ issue }: { issue: Issue }) {
  const [deleteOpen, setDeleteOpen] = useState(false);
  const { updateIssue, deleteIssue, isMutating } = useIssueMutations();
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const user = useAuthStore((s) => s.user);
  const currentMember = members.find((m) => m.user_id === user?.id);
  const currentMemberRole = currentMember?.role;

  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      void updateIssue(issue.id, updates).catch(() =>
        toast.error("Failed to update issue"),
      );
    },
    [issue.id, updateIssue],
  );

  const handleDelete = async () => {
    await deleteIssue(issue.id).catch(() =>
      toast.error("Failed to delete issue"),
    );
  };

  return (
    <PickerWrapper>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              className="rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground group-hover:opacity-100"
              aria-label="Issue actions"
            >
              <MoreHorizontal className="size-3.5" />
            </button>
          }
        />

        <DropdownMenuContent align="end" className="w-auto">
          {/* Status */}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
              Status
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              {ALL_STATUSES.map((s) => (
                <DropdownMenuItem
                  key={s}
                  onClick={() => handleUpdate({ status: s })}
                >
                  <StatusIcon status={s} className="h-3.5 w-3.5" />
                  {STATUS_CONFIG[s].label}
                  {issue.status === s && (
                    <Check className="ml-auto size-3.5 text-muted-foreground" />
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          {/* Priority */}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <PriorityIcon priority={issue.priority} />
              Priority
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              {PRIORITY_ORDER.map((p) => (
                <DropdownMenuItem
                  key={p}
                  onClick={() => handleUpdate({ priority: p })}
                >
                  <span
                    className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
                  >
                    <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                    {PRIORITY_CONFIG[p].label}
                  </span>
                  {issue.priority === p && (
                    <Check className="ml-auto size-3.5 text-muted-foreground" />
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          {/* Assignee */}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <UserMinus className="h-3.5 w-3.5" />
              Assignee
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuItem
                onClick={() =>
                  handleUpdate({ assignee_type: null, assignee_id: null })
                }
              >
                <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                Unassigned
                {!issue.assignee_type && (
                  <Check className="ml-auto size-3.5 text-muted-foreground" />
                )}
              </DropdownMenuItem>
              {members.map((m) => (
                <DropdownMenuItem
                  key={m.user_id}
                  onClick={() =>
                    handleUpdate({
                      assignee_type: "member",
                      assignee_id: m.user_id,
                    })
                  }
                >
                  <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                  {m.name}
                  {issue.assignee_type === "member" &&
                    issue.assignee_id === m.user_id && (
                      <Check className="ml-auto size-3.5 text-muted-foreground" />
                    )}
                </DropdownMenuItem>
              ))}
              {agents
                .filter(
                  (a) =>
                    !a.archived_at &&
                    canAssignAgent(a, user?.id, currentMemberRole),
                )
                .map((a) => (
                  <DropdownMenuItem
                    key={a.id}
                    onClick={() =>
                      handleUpdate({
                        assignee_type: "agent",
                        assignee_id: a.id,
                      })
                    }
                  >
                    <ActorAvatar actorType="agent" actorId={a.id} size={16} />
                    {a.name}
                    {issue.assignee_type === "agent" &&
                      issue.assignee_id === a.id && (
                        <Check className="ml-auto size-3.5 text-muted-foreground" />
                      )}
                  </DropdownMenuItem>
                ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          {/* Due date */}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Calendar className="h-3.5 w-3.5" />
              Due date
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuItem
                onClick={() =>
                  handleUpdate({ due_date: new Date().toISOString() })
                }
              >
                Today
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  const d = new Date();
                  d.setDate(d.getDate() + 1);
                  handleUpdate({ due_date: d.toISOString() });
                }}
              >
                Tomorrow
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  const d = new Date();
                  d.setDate(d.getDate() + 7);
                  handleUpdate({ due_date: d.toISOString() });
                }}
              >
                Next week
              </DropdownMenuItem>
              {issue.due_date && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onClick={() => handleUpdate({ due_date: null })}
                  >
                    Clear date
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuSubContent>
          </DropdownMenuSub>

          <DropdownMenuSeparator />

          {/* Create from template */}
          <DropdownMenuItem
            onClick={() =>
              useModalStore
                .getState()
                .open("create-issue", buildIssueTemplateData(issue))
            }
          >
            <Copy className="h-3.5 w-3.5" />
            Create from template
          </DropdownMenuItem>

          {/* Copy link */}
          <DropdownMenuItem
            onClick={() => {
              navigator.clipboard.writeText(
                `${window.location.origin}/issues/${issue.id}`,
              );
              toast.success("Link copied");
            }}
          >
            <Link2 className="h-3.5 w-3.5" />
            Copy link
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          {/* Delete */}
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
          >
            <Trash2 className="h-3.5 w-3.5" />
            Delete issue
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete issue</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently delete this issue and all its comments. This
              action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={isMutating}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {isMutating ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PickerWrapper>
  );
}

// ---------------------------------------------------------------------------
// BoardCardContent
// ---------------------------------------------------------------------------

export const BoardCardContent = memo(function BoardCardContent({
  issue,
  editable = false,
}: {
  issue: Issue;
  editable?: boolean;
}) {
  const storeProperties = useViewStore((s) => s.cardProperties);
  const priorityCfg = PRIORITY_CONFIG[issue.priority];
  const { updateIssue } = useIssueMutations();

  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      void updateIssue(issue.id, updates).catch(() => {
        toast.error("Failed to update issue");
      });
    },
    [issue.id, updateIssue],
  );

  const showPriority = storeProperties.priority;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const scheduleLabel = storeProperties.dueDate ? formatIssueSchedule(issue) : null;
  const showSchedule = !!scheduleLabel;
  const canEditDueDate = editable && !!issue.due_date;

  return (
    <div className="rounded-lg border bg-card p-3.5 shadow-[0_1px_2px_0_rgba(0,0,0,0.03)] transition-shadow group-hover:shadow-sm">
      {/* Row 1: Identifier + actions menu */}
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">{issue.identifier}</p>
        {editable && <BoardCardActions issue={issue} />}
      </div>

      {/* Row 2: Title */}
      <p className="mt-1 text-sm font-medium leading-snug line-clamp-2">
        {issue.title}
      </p>

      <IssueTaskStatusBadge issue={issue} variant="board" />

      {/* Row 3: Assignee, priority badge, schedule */}
      {(showAssignee || showPriority || showSchedule) && (
        <div className="mt-3 flex items-center gap-2">
          {showAssignee &&
            (editable ? (
              <PickerWrapper>
                <AssigneePicker
                  assigneeType={issue.assignee_type}
                  assigneeId={issue.assignee_id}
                  onUpdate={handleUpdate}
                  trigger={
                    <ActorAvatar
                      actorType={issue.assignee_type!}
                      actorId={issue.assignee_id!}
                      size={22}
                    />
                  }
                />
              </PickerWrapper>
            ) : (
              <ActorAvatar
                actorType={issue.assignee_type!}
                actorId={issue.assignee_id!}
                size={22}
              />
            ))}
          {showPriority &&
            (editable ? (
              <PickerWrapper>
                <PriorityPicker
                  priority={issue.priority}
                  onUpdate={handleUpdate}
                  trigger={
                    <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${priorityCfg.badgeBg} ${priorityCfg.badgeText}`}>
                      <PriorityIcon priority={issue.priority} className="h-3 w-3" inheritColor />
                      {priorityCfg.label}
                    </span>
                  }
                />
              </PickerWrapper>
            ) : (
              <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${priorityCfg.badgeBg} ${priorityCfg.badgeText}`}>
                <PriorityIcon priority={issue.priority} className="h-3 w-3" inheritColor />
                {priorityCfg.label}
              </span>
            ))}
          {showSchedule && (
            <div className="ml-auto">
              {canEditDueDate ? (
                <PickerWrapper>
                  <DueDatePicker
                    dueDate={issue.due_date}
                    onUpdate={handleUpdate}
                    trigger={
                      <span
                        className={`flex items-center gap-1 text-xs ${
                          isIssueScheduleOverdue(issue)
                            ? "text-destructive"
                            : "text-muted-foreground"
                        }`}
                      >
                        <CalendarDays className="size-3" />
                        {scheduleLabel}
                      </span>
                    }
                  />
                </PickerWrapper>
              ) : (
                <span
                  className={`flex items-center gap-1 text-xs ${
                    isIssueScheduleOverdue(issue)
                      ? "text-destructive"
                      : "text-muted-foreground"
                  }`}
                >
                  <CalendarDays className="size-3" />
                  {scheduleLabel}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
});

export const DraggableBoardCard = memo(function DraggableBoardCard({ issue }: { issue: Issue }) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: issue.id,
    data: { status: issue.status },
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      className={isDragging ? "opacity-30" : ""}
    >
      <Link
        href={`/issues/${issue.id}`}
        className={`group block transition-colors ${isDragging ? "pointer-events-none" : ""}`}
      >
        <BoardCardContent issue={issue} editable />
      </Link>
    </div>
  );
});
