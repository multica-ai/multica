"use client";

import { useCallback } from "react";
import Link from "next/link";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@/shared/types";
import { CalendarDays } from "lucide-react";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { api } from "@/shared/api";
import { useIssueStore } from "@/features/issues/store";
import { PriorityIcon } from "./priority-icon";
import { PriorityPicker, AssigneePicker, DueDatePicker } from "./pickers";
import { PRIORITY_CONFIG } from "@/features/issues/config";
import { useIssueViewStore, type CardProperties } from "@/features/issues/stores/view-store";

function formatDate(date: string): string {
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

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

export function BoardCardContent({
  issue,
  editable = false,
}: {
  issue: Issue;
  editable?: boolean;
}) {
  const storeProperties = useIssueViewStore((s) => s.cardProperties);
  const priorityCfg = PRIORITY_CONFIG[issue.priority];

  const handleUpdate = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      useIssueStore.getState().updateIssue(issue.id, updates);
      api.updateIssue(issue.id, updates).catch(() => {
        toast.error("Failed to update issue");
      });
    },
    [issue.id]
  );

  const showPriority = storeProperties.priority;
  const showDescription = storeProperties.description && issue.description;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showBottom = showAssignee || showDueDate;

  return (
    <div className="rounded-lg border bg-card p-3.5 shadow-[0_1px_2px_0_rgba(0,0,0,0.03)] transition-shadow group-hover:shadow-md">
      {/* Priority */}
      {showPriority &&
        (editable ? (
          <PickerWrapper>
            <PriorityPicker
              priority={issue.priority}
              onUpdate={handleUpdate}
              trigger={
                <>
                  <PriorityIcon priority={issue.priority} />
                  <span className={`text-xs font-medium ${priorityCfg.color}`}>
                    {priorityCfg.label}
                  </span>
                </>
              }
            />
          </PickerWrapper>
        ) : (
          <div className="flex items-center gap-1.5">
            <PriorityIcon priority={issue.priority} />
            <span className={`text-xs font-medium ${priorityCfg.color}`}>
              {priorityCfg.label}
            </span>
          </div>
        ))}

      {/* Title */}
      <p
        className={`text-sm font-medium leading-snug line-clamp-2 ${showPriority ? "mt-2" : ""}`}
      >
        {issue.title}
      </p>

      {/* Description */}
      {showDescription && (
        <p className="mt-1 text-xs text-muted-foreground line-clamp-1">
          {issue.description}
        </p>
      )}

      {/* Bottom: assignee + due date */}
      {showBottom && (
        <div className="mt-3 flex items-center justify-between">
          <div className="flex items-center">
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
          </div>
          {showDueDate &&
            (editable ? (
              <PickerWrapper>
                <DueDatePicker
                  dueDate={issue.due_date}
                  onUpdate={handleUpdate}
                  trigger={
                    <span
                      className={`flex items-center gap-1 text-xs ${
                        new Date(issue.due_date!) < new Date()
                          ? "text-destructive"
                          : "text-muted-foreground"
                      }`}
                    >
                      <CalendarDays className="size-3" />
                      {formatDate(issue.due_date!)}
                    </span>
                  }
                />
              </PickerWrapper>
            ) : (
              <span
                className={`flex items-center gap-1 text-xs ${
                  new Date(issue.due_date!) < new Date()
                    ? "text-destructive"
                    : "text-muted-foreground"
                }`}
              >
                <CalendarDays className="size-3" />
                {formatDate(issue.due_date!)}
              </span>
            ))}
        </div>
      )}
    </div>
  );
}

export function DraggableBoardCard({ issue }: { issue: Issue }) {
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
}
