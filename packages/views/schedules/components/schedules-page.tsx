"use client";

import { useState, useCallback, useMemo } from "react";
import { Plus, Clock, ChevronRight, Trash2, Power, PowerOff, Pencil } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { scheduleListOptions } from "@multica/core/schedules/queries";
import { workflowListOptions } from "@multica/core/workflows/queries";
import { useCreateSchedule, useDeleteSchedule, useToggleSchedule, useUpdateSchedule } from "@multica/core/schedules/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { Schedule, ScheduleType } from "@multica/core/types";

const WEEKDAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

function formatScheduleInfo(schedule: Schedule): string {
  if (schedule.schedule_type === "once") {
    if (schedule.once_at) {
      return `Once at ${new Date(schedule.once_at).toLocaleString()}`;
    }
    return "Once";
  }
  if (schedule.weekdays?.length > 0) {
    const days = schedule.weekdays.map((d) => WEEKDAY_LABELS[d]).join(", ");
    return `Every ${days} at ${schedule.time_of_day || "—"}`;
  }
  if (schedule.cron_expr) {
    return `Cron: ${schedule.cron_expr}`;
  }
  return "Recurring";
}

function ScheduleRow({
  schedule,
  workflowName,
  onToggle,
  onDelete,
  onRename,
}: {
  schedule: Schedule;
  workflowName: string;
  onToggle: (id: string) => void;
  onDelete: (id: string) => void;
  onRename: (id: string, name: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(schedule.name);

  const commitRename = useCallback(() => {
    const trimmed = editName.trim();
    if (trimmed && trimmed !== schedule.name) {
      onRename(schedule.id, trimmed);
    }
    setEditing(false);
  }, [editName, schedule.name, schedule.id, onRename]);

  return (
    <div className="group/row flex h-14 items-center gap-3 px-5 text-sm transition-colors hover:bg-accent/40 border-b border-border/40">
      <div className={cn("h-2 w-2 rounded-full shrink-0", schedule.enabled ? "bg-green-500" : "bg-muted-foreground/40")} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          {editing ? (
            <input
              className="min-w-0 flex-1 rounded border bg-transparent px-2 py-0.5 text-sm font-medium outline-none focus:ring-1 focus:ring-ring"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              onBlur={commitRename}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitRename();
                if (e.key === "Escape") { setEditName(schedule.name); setEditing(false); }
              }}
              autoFocus
            />
          ) : (
            <>
              <span className="font-medium truncate">{schedule.name}</span>
              <span className="text-xs text-muted-foreground">→ {workflowName}</span>
            </>
          )}
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">
          {formatScheduleInfo(schedule)}
          {schedule.next_run_at && schedule.enabled && (
            <span className="ml-2">• Next: {new Date(schedule.next_run_at).toLocaleString()}</span>
          )}
        </p>
      </div>
      <div className="flex items-center gap-1 opacity-0 group-hover/row:opacity-100 transition-opacity">
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={() => { setEditName(schedule.name); setEditing(true); }}
          title="Rename"
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={() => onToggle(schedule.id)}
          title={schedule.enabled ? "Disable" : "Enable"}
        >
          {schedule.enabled ? (
            <PowerOff className="h-3.5 w-3.5" />
          ) : (
            <Power className="h-3.5 w-3.5" />
          )}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-destructive"
          onClick={() => onDelete(schedule.id)}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}

export function SchedulesPage() {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const { data: schedules = [], isLoading } = useQuery(scheduleListOptions(wsId));
  const { data: workflows = [] } = useQuery(workflowListOptions(wsId));
  const createSchedule = useCreateSchedule();
  const deleteSchedule = useDeleteSchedule();
  const toggleSchedule = useToggleSchedule();
  const updateSchedule = useUpdateSchedule();

  const workflowMap = useMemo(
    () => new Map(workflows.map((w) => [w.id, w.name])),
    [workflows],
  );

  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [workflowId, setWorkflowId] = useState("");
  const [scheduleType, setScheduleType] = useState<ScheduleType>("recurring");
  const [weekdays, setWeekdays] = useState<number[]>([]);
  const [timeOfDay, setTimeOfDay] = useState("09:00");
  const [onceAt, setOnceAt] = useState("");

  const resetForm = useCallback(() => {
    setName("");
    setDescription("");
    setWorkflowId("");
    setScheduleType("recurring");
    setWeekdays([]);
    setTimeOfDay("09:00");
    setOnceAt("");
  }, []);

  const toggleWeekday = useCallback((day: number) => {
    setWeekdays((prev) =>
      prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day].sort(),
    );
  }, []);

  const handleCreate = useCallback(() => {
    if (!name.trim() || !workflowId) return;
    createSchedule.mutate(
      {
        workflow_id: workflowId,
        name: name.trim(),
        description: description.trim(),
        schedule_type: scheduleType,
        weekdays: scheduleType === "recurring" ? weekdays : undefined,
        time_of_day: scheduleType === "recurring" ? timeOfDay : undefined,
        once_at: scheduleType === "once" ? onceAt : undefined,
      },
      {
        onSuccess: () => {
          setDialogOpen(false);
          resetForm();
          toast.success("Schedule created");
        },
        onError: () => toast.error("Failed to create schedule"),
      },
    );
  }, [name, description, workflowId, scheduleType, weekdays, timeOfDay, onceAt, createSchedule, resetForm]);

  const handleToggle = useCallback(
    (id: string) => {
      toggleSchedule.mutate(id, {
        onError: () => toast.error("Failed to toggle schedule"),
      });
    },
    [toggleSchedule],
  );

  const handleDelete = useCallback(
    (id: string) => {
      deleteSchedule.mutate(id, {
        onSuccess: () => toast.success("Schedule deleted"),
        onError: () => toast.error("Failed to delete schedule"),
      });
    },
    [deleteSchedule],
  );

  const handleRename = useCallback(
    (id: string, name: string) => {
      updateSchedule.mutate(
        { id, name },
        {
          onSuccess: () => toast.success("Schedule renamed"),
          onError: () => toast.error("Failed to rename schedule"),
        },
      );
    },
    [updateSchedule],
  );

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
          <Skeleton className="h-5 w-40" />
        </div>
        <div className="p-4 space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Breadcrumb */}
      <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
        <WorkspaceAvatar name={workspace?.name ?? "W"} size="sm" />
        <span className="text-sm text-muted-foreground">{workspace?.name ?? "Workspace"}</span>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        <span className="text-sm font-medium">Schedules</span>
      </div>

      {/* Header */}
      <div className="flex h-11 shrink-0 items-center justify-between border-b px-4">
        <span className="text-sm text-muted-foreground">
          {schedules.length} schedule{schedules.length !== 1 ? "s" : ""}
        </span>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1.5"
          onClick={() => setDialogOpen(true)}
          disabled={workflows.length === 0}
        >
          <Plus className="h-3.5 w-3.5" />
          New Schedule
        </Button>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto">
        {schedules.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground pt-20">
            <Clock className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">No schedules yet</p>
            <p className="text-xs">
              {workflows.length === 0
                ? "Create a workflow first, then set up schedules."
                : "Create a schedule to trigger workflows automatically."}
            </p>
          </div>
        ) : (
          schedules.map((s) => (
            <ScheduleRow
              key={s.id}
              schedule={s}
              workflowName={workflowMap.get(s.workflow_id) ?? "Unknown"}
              onToggle={handleToggle}
              onDelete={handleDelete}
              onRename={handleRename}
            />
          ))
        )}
      </div>

      {/* Create Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogTitle>New Schedule</DialogTitle>
          <div className="space-y-3 pt-2">
            <div>
              <label className="text-sm font-medium">Name</label>
              <input
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                placeholder="e.g. Weekly Code Review"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
              />
            </div>

            <div>
              <label className="text-sm font-medium">Workflow</label>
              <select
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                value={workflowId}
                onChange={(e) => setWorkflowId(e.target.value)}
              >
                <option value="">Select a workflow...</option>
                {workflows.map((w) => (
                  <option key={w.id} value={w.id}>{w.name}</option>
                ))}
              </select>
            </div>

            <div>
              <label className="text-sm font-medium">Type</label>
              <div className="flex gap-2 mt-1">
                <button
                  type="button"
                  className={cn(
                    "flex-1 rounded-md border px-3 py-2 text-sm transition-colors",
                    scheduleType === "recurring" ? "border-ring bg-accent" : "hover:bg-accent/40",
                  )}
                  onClick={() => setScheduleType("recurring")}
                >
                  Recurring
                </button>
                <button
                  type="button"
                  className={cn(
                    "flex-1 rounded-md border px-3 py-2 text-sm transition-colors",
                    scheduleType === "once" ? "border-ring bg-accent" : "hover:bg-accent/40",
                  )}
                  onClick={() => setScheduleType("once")}
                >
                  Once
                </button>
              </div>
            </div>

            {scheduleType === "recurring" && (
              <>
                <div>
                  <label className="text-sm font-medium">Days</label>
                  <div className="flex gap-1 mt-1">
                    {WEEKDAY_LABELS.map((label, day) => (
                      <button
                        key={day}
                        type="button"
                        className={cn(
                          "h-8 w-10 rounded text-xs font-medium transition-colors",
                          weekdays.includes(day)
                            ? "bg-primary text-primary-foreground"
                            : "border hover:bg-accent/40",
                        )}
                        onClick={() => toggleWeekday(day)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </div>
                <div>
                  <label className="text-sm font-medium">Time</label>
                  <input
                    type="time"
                    className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    value={timeOfDay}
                    onChange={(e) => setTimeOfDay(e.target.value)}
                  />
                </div>
              </>
            )}

            {scheduleType === "once" && (
              <div>
                <label className="text-sm font-medium">Date & Time</label>
                <input
                  type="datetime-local"
                  className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                  value={onceAt}
                  onChange={(e) => setOnceAt(e.target.value)}
                />
              </div>
            )}

            <div>
              <label className="text-sm font-medium">Description</label>
              <textarea
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-none"
                placeholder="Optional description..."
                rows={2}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>

            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => { setDialogOpen(false); resetForm(); }}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleCreate}
                disabled={!name.trim() || !workflowId}
              >
                Create
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
