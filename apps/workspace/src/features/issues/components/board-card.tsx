"use client";

import { useCallback, memo } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@/shared/types";
import { CalendarDays } from "lucide-react";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useIssueMutations } from "@/features/issues/mutations";
import {
  formatIssueSchedule,
  isIssueScheduleOverdue,
} from "@/features/issues/utils/workbench-view";
import { PriorityIcon } from "./priority-icon";
import { PriorityPicker, AssigneePicker, DueDatePicker } from "./pickers";
import { PRIORITY_CONFIG } from "@/features/issues/config";
import { useViewStore } from "@/features/issues/stores/view-store-context";
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
      {/* Row 1: Identifier */}
      <p className="text-xs text-muted-foreground">{issue.identifier}</p>

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
