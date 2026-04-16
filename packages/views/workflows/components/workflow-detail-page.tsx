"use client";

import { useState, useCallback } from "react";
import {
  ChevronRight,
  Plus,
  Trash2,
  Bot,
  ShieldCheck,
  ArrowDown,
  Play,
  Loader2,
  CheckCircle2,
  XCircle,
  PauseCircle,
  Clock,
  GripVertical,
  Timer,
  Power,
  PowerOff,
  Pencil,
} from "lucide-react";
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useQuery } from "@tanstack/react-query";
import { workflowDetailOptions, workflowRunsOptions, workflowRunDetailOptions } from "@multica/core/workflows/queries";
import { useUpdateWorkflow, useTriggerWorkflow, useCancelWorkflowRun, useApproveStepRun, useSubmitReview } from "@multica/core/workflows/mutations";
import { scheduleListForWorkflowOptions } from "@multica/core/schedules/queries";
import { useCreateSchedule, useDeleteSchedule, useToggleSchedule, useUpdateSchedule } from "@multica/core/schedules/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths, useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { AppLink } from "../../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import type { WorkflowStep, WorkflowStepType, WorkflowRun, WorkflowStepRun, ApprovalDecision, ReviewDecision, Schedule, ScheduleType } from "@multica/core/types";

const RUN_STATUS_CONFIG: Record<string, { icon: React.ElementType; color: string; label: string }> = {
  pending: { icon: Clock, color: "text-muted-foreground", label: "Pending" },
  running: { icon: Loader2, color: "text-blue-500", label: "Running" },
  paused: { icon: PauseCircle, color: "text-yellow-500", label: "Awaiting Approval" },
  planning: { icon: Loader2, color: "text-purple-500", label: "Planning" },
  completed: { icon: CheckCircle2, color: "text-green-500", label: "Completed" },
  failed: { icon: XCircle, color: "text-red-500", label: "Failed" },
  cancelled: { icon: XCircle, color: "text-muted-foreground", label: "Cancelled" },
};

function formatDuration(startedAt: string | null, completedAt: string | null): string | null {
  if (!startedAt) return null;
  const start = new Date(startedAt).getTime();
  const end = completedAt ? new Date(completedAt).getTime() : Date.now();
  const ms = end - start;
  if (ms < 1000) return `${ms}ms`;
  const secs = Math.floor(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  const remSecs = secs % 60;
  if (mins < 60) return `${mins}m ${remSecs}s`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return `${hours}h ${remMins}m`;
}

function StepCard({
  step,
  index,
  agents,
  onRemove,
  sortableId,
  showArrow,
}: {
  step: WorkflowStep;
  index: number;
  agents: { id: string; name: string }[];
  onRemove: () => void;
  sortableId: string;
  showArrow: boolean;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: sortableId });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    position: "relative" as const,
    zIndex: isDragging ? 50 : undefined,
  };
  const agentName = step.agent_id
    ? agents.find((a) => a.id === step.agent_id)?.name ?? "Unknown Agent"
    : null;

  return (
    <div ref={setNodeRef} style={style}>
      {showArrow && (
        <div className="flex justify-center py-1">
          <ArrowDown className="h-4 w-4 text-muted-foreground" />
        </div>
      )}
      <div className={cn("group relative flex items-start gap-3 rounded-lg border bg-card p-3", isDragging && "opacity-30 shadow-lg")}>
        <button
          type="button"
          className="flex h-8 w-5 shrink-0 items-center justify-center cursor-grab active:cursor-grabbing text-muted-foreground/50 hover:text-muted-foreground"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="h-4 w-4" />
        </button>
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent">
          {step.type === "agent" ? (
            <Bot className="h-4 w-4" />
          ) : step.type === "review" ? (
            <ShieldCheck className="h-4 w-4 text-purple-500" />
          ) : (
            <ShieldCheck className="h-4 w-4" />
          )}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground">Step {index + 1}</span>
            <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-accent">
              {step.type === "agent" ? "Agent" : step.type === "review" ? "Review" : "Approval"}
            </span>
          </div>
          {step.type === "agent" && (
            <p className="mt-1 text-sm truncate">
              {agentName && <span className="font-medium">{agentName}</span>}
              {step.prompt && <span className="text-muted-foreground ml-1">— {step.prompt}</span>}
            </p>
          )}
          {step.type === "approval" && (
            <p className="mt-1 text-sm text-muted-foreground">
              Requires manual approval before continuing
            </p>
          )}
          {step.type === "review" && (
            <p className="mt-1 text-sm text-muted-foreground">
              {step.reviewer_type === "agent" ? "AI review gate" : "Manual review gate"}
              {step.review_prompt && <span className="ml-1">— {step.review_prompt}</span>}
            </p>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity text-destructive"
          onClick={onRemove}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}

function RunRow({
  run,
  onSelect,
}: {
  run: WorkflowRun;
  onSelect: (id: string) => void;
}) {
  const cfg = RUN_STATUS_CONFIG[run.status] ?? RUN_STATUS_CONFIG.pending!;
  const Icon = cfg.icon;
  const duration = formatDuration(run.started_at, run.completed_at);

  return (
    <button
      type="button"
      className="flex h-10 w-full items-center gap-3 px-3 text-sm hover:bg-accent/40 transition-colors text-left"
      onClick={() => onSelect(run.id)}
    >
      <Icon className={cn("h-4 w-4 shrink-0", cfg.color, run.status === "running" && "animate-spin")} />
      <span className="flex-1 truncate">{cfg.label}</span>
      {duration && (
        <span className="flex items-center gap-0.5 text-xs text-muted-foreground shrink-0">
          <Timer className="h-3 w-3" />
          {duration}
        </span>
      )}
      <span className="text-xs text-muted-foreground shrink-0">
        {new Date(run.created_at).toLocaleString()}
      </span>
    </button>
  );
}

function StepRunStatus({ stepRun }: { stepRun: WorkflowStepRun }) {
  const cfg = RUN_STATUS_CONFIG[stepRun.status] ?? RUN_STATUS_CONFIG.pending!;
  const Icon = cfg.icon;
  const duration = formatDuration(stepRun.started_at, stepRun.completed_at);
  const isRunning = stepRun.status === "running";

  return (
    <div className="flex items-center gap-3 rounded-lg border p-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent">
        {stepRun.step_type === "agent" ? (
          <Bot className="h-4 w-4" />
        ) : stepRun.step_type === "review" ? (
          <ShieldCheck className="h-4 w-4 text-purple-500" />
        ) : stepRun.step_type === "planner" ? (
          <Bot className="h-4 w-4 text-purple-500" />
        ) : (
          <ShieldCheck className="h-4 w-4" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {stepRun.step_type === "planner" ? "Planner" : `Step ${stepRun.step_index + 1}`}
          </span>
          <span className="text-xs font-medium px-1.5 py-0.5 rounded bg-accent">
            {stepRun.step_type === "agent" ? "Agent" : stepRun.step_type === "review" ? "Review" : stepRun.step_type === "planner" ? "Planner" : "Approval"}
          </span>
        </div>
        <div className="flex items-center gap-1.5 mt-1">
          <Icon className={cn("h-3.5 w-3.5", cfg.color, isRunning && "animate-spin")} />
          <span className="text-sm">{cfg.label}</span>
          {stepRun.decision && (
            <span className={cn(
              "text-xs px-1.5 py-0.5 rounded",
              stepRun.decision === "approved" ? "bg-green-500/10 text-green-600" :
              stepRun.decision === "rejected" ? "bg-yellow-500/10 text-yellow-600" :
              "bg-red-500/10 text-red-600",
            )}>
              {stepRun.decision}
            </span>
          )}
        </div>
      </div>
      {duration && (
        <div className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
          <Timer className="h-3 w-3" />
          <span>{duration}</span>
          {isRunning && <span className="animate-pulse">…</span>}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Schedule helpers
// ---------------------------------------------------------------------------
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
  onToggle,
  onDelete,
  onRename,
}: {
  schedule: Schedule;
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
    <div className="group/srow flex items-center gap-2 rounded-lg border p-3 text-sm">
      <div className={cn("h-2 w-2 rounded-full shrink-0", schedule.enabled ? "bg-green-500" : "bg-muted-foreground/40")} />
      <div className="flex-1 min-w-0">
        {editing ? (
          <input
            className="w-full rounded border bg-transparent px-1.5 py-0.5 text-xs font-medium outline-none focus:ring-1 focus:ring-ring"
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
          <span className="text-xs font-medium truncate block">{schedule.name}</span>
        )}
        <p className="text-[11px] text-muted-foreground mt-0.5 truncate">
          {formatScheduleInfo(schedule)}
          {schedule.next_run_at && schedule.enabled && (
            <span className="ml-1">• Next: {new Date(schedule.next_run_at).toLocaleString()}</span>
          )}
        </p>
      </div>
      <div className="flex items-center gap-0.5 opacity-0 group-hover/srow:opacity-100 transition-opacity shrink-0">
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => { setEditName(schedule.name); setEditing(true); }} title="Rename">
          <Pencil className="h-3 w-3" />
        </Button>
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onToggle(schedule.id)} title={schedule.enabled ? "Disable" : "Enable"}>
          {schedule.enabled ? <PowerOff className="h-3 w-3" /> : <Power className="h-3 w-3" />}
        </Button>
        <Button variant="ghost" size="icon" className="h-6 w-6 text-destructive" onClick={() => onDelete(schedule.id)}>
          <Trash2 className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}

export function WorkflowDetailPage({ workflowId }: { workflowId: string }) {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const p = useWorkspacePaths();
  const { data: workflow, isLoading } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runs = [] } = useQuery(workflowRunsOptions(wsId, workflowId));
  const { data: schedules = [] } = useQuery(scheduleListForWorkflowOptions(wsId, workflowId));
  const updateWorkflow = useUpdateWorkflow();
  const triggerMutation = useTriggerWorkflow();
  const cancelRun = useCancelWorkflowRun();
  const approveStep = useApproveStepRun();
  const submitReview = useSubmitReview();
  const createSchedule = useCreateSchedule();
  const deleteScheduleMut = useDeleteSchedule();
  const toggleScheduleMut = useToggleSchedule();
  const updateScheduleMut = useUpdateSchedule();

  const [addStepType, setAddStepType] = useState<WorkflowStepType | null>(null);
  const [stepAgentId, setStepAgentId] = useState("");
  const [stepPrompt, setStepPrompt] = useState("");
  const [reviewAgentId, setReviewAgentId] = useState("");
  const [reviewPrompt, setReviewPrompt] = useState("");
  const [reviewerType, setReviewerType] = useState<"agent" | "member">("agent");
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState(false);
  const [editName, setEditName] = useState("");
  const [rightTab, setRightTab] = useState<"runs" | "schedules">("runs");

  // Schedule form state
  const [scheduleDialogOpen, setScheduleDialogOpen] = useState(false);
  const [scheduleName, setScheduleName] = useState("");
  const [scheduleDesc, setScheduleDesc] = useState("");
  const [scheduleType, setScheduleType] = useState<ScheduleType>("recurring");
  const [scheduleWeekdays, setScheduleWeekdays] = useState<number[]>([]);
  const [scheduleTimeOfDay, setScheduleTimeOfDay] = useState("09:00");
  const [scheduleOnceAt, setScheduleOnceAt] = useState("");

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const stepIds = workflow?.steps.map((_, i) => `step-${i}`) ?? [];

  const { data: selectedRun } = useQuery({
    ...workflowRunDetailOptions(wsId, selectedRunId ?? ""),
    enabled: !!selectedRunId,
  });

  const handleAddStep = useCallback(() => {
    if (!workflow || !addStepType) return;
    let newStep: WorkflowStep;
    if (addStepType === "agent") {
      newStep = { type: "agent", agent_id: stepAgentId || undefined, prompt: stepPrompt || undefined };
    } else if (addStepType === "review") {
      newStep = {
        type: "review",
        reviewer_type: reviewerType,
        review_agent_id: reviewerType === "agent" ? reviewAgentId || undefined : undefined,
        review_prompt: reviewPrompt || undefined,
      };
    } else {
      newStep = { type: "approval" };
    }

    updateWorkflow.mutate(
      { id: workflowId, steps: [...workflow.steps, newStep] },
      {
        onSuccess: () => {
          setAddStepType(null);
          setStepAgentId("");
          setStepPrompt("");
          setReviewAgentId("");
          setReviewPrompt("");
          setReviewerType("agent");
          toast.success("Step added");
        },
        onError: () => toast.error("Failed to add step"),
      },
    );
  }, [workflow, addStepType, stepAgentId, stepPrompt, reviewerType, reviewAgentId, reviewPrompt, updateWorkflow, workflowId]);

  const handleRemoveStep = useCallback(
    (index: number) => {
      if (!workflow) return;
      const newSteps = workflow.steps.filter((_, i) => i !== index);
      updateWorkflow.mutate(
        { id: workflowId, steps: newSteps },
        {
          onSuccess: () => toast.success("Step removed"),
          onError: () => toast.error("Failed to remove step"),
        },
      );
    },
    [workflow, updateWorkflow, workflowId],
  );

  const handleReorder = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id || !workflow) return;
      const oldIndex = stepIds.indexOf(active.id as string);
      const newIndex = stepIds.indexOf(over.id as string);
      if (oldIndex === -1 || newIndex === -1) return;
      const newSteps = arrayMove(workflow.steps, oldIndex, newIndex);
      updateWorkflow.mutate(
        { id: workflowId, steps: newSteps },
        {
          onError: () => toast.error("Failed to reorder steps"),
        },
      );
    },
    [workflow, stepIds, updateWorkflow, workflowId],
  );

  const handleTrigger = useCallback(() => {
    triggerMutation.mutate(workflowId, {
      onSuccess: () => toast.success("Workflow triggered"),
      onError: () => toast.error("Failed to trigger workflow"),
    });
  }, [triggerMutation, workflowId]);

  const handleApproval = useCallback(
    (stepRunId: string, decision: ApprovalDecision) => {
      approveStep.mutate(
        { stepRunId, decision },
        {
          onSuccess: () => toast.success("Decision submitted"),
          onError: () => toast.error("Failed to submit decision"),
        },
      );
    },
    [approveStep],
  );

  const handleReview = useCallback(
    (stepRunId: string, decision: ReviewDecision) => {
      submitReview.mutate(
        { stepRunId, decision },
        {
          onSuccess: () => toast.success("Review decision submitted"),
          onError: () => toast.error("Failed to submit review"),
        },
      );
    },
    [submitReview],
  );

  const commitRename = useCallback(() => {
    const trimmed = editName.trim();
    if (trimmed && trimmed !== workflow?.name) {
      updateWorkflow.mutate(
        { id: workflowId, name: trimmed },
        {
          onSuccess: () => toast.success("Workflow renamed"),
          onError: () => toast.error("Failed to rename"),
        },
      );
    }
    setEditingName(false);
  }, [editName, workflow?.name, updateWorkflow, workflowId]);

  // Schedule handlers
  const resetScheduleForm = useCallback(() => {
    setScheduleName("");
    setScheduleDesc("");
    setScheduleType("recurring");
    setScheduleWeekdays([]);
    setScheduleTimeOfDay("09:00");
    setScheduleOnceAt("");
  }, []);

  const toggleWeekday = useCallback((day: number) => {
    setScheduleWeekdays((prev) =>
      prev.includes(day) ? prev.filter((d) => d !== day) : [...prev, day].sort(),
    );
  }, []);

  const handleCreateSchedule = useCallback(() => {
    if (!scheduleName.trim()) return;
    createSchedule.mutate(
      {
        workflow_id: workflowId,
        name: scheduleName.trim(),
        description: scheduleDesc.trim(),
        schedule_type: scheduleType,
        weekdays: scheduleType === "recurring" ? scheduleWeekdays : undefined,
        time_of_day: scheduleType === "recurring" ? scheduleTimeOfDay : undefined,
        once_at: scheduleType === "once" ? scheduleOnceAt : undefined,
      },
      {
        onSuccess: () => {
          setScheduleDialogOpen(false);
          resetScheduleForm();
          toast.success("Schedule created");
        },
        onError: () => toast.error("Failed to create schedule"),
      },
    );
  }, [scheduleName, scheduleDesc, scheduleType, scheduleWeekdays, scheduleTimeOfDay, scheduleOnceAt, workflowId, createSchedule, resetScheduleForm]);

  const handleToggleSchedule = useCallback(
    (id: string) => {
      toggleScheduleMut.mutate(id, {
        onError: () => toast.error("Failed to toggle schedule"),
      });
    },
    [toggleScheduleMut],
  );

  const handleDeleteSchedule = useCallback(
    (id: string) => {
      deleteScheduleMut.mutate(id, {
        onSuccess: () => toast.success("Schedule deleted"),
        onError: () => toast.error("Failed to delete schedule"),
      });
    },
    [deleteScheduleMut],
  );

  const handleRenameSchedule = useCallback(
    (id: string, name: string) => {
      updateScheduleMut.mutate(
        { id, name },
        {
          onSuccess: () => toast.success("Schedule renamed"),
          onError: () => toast.error("Failed to rename schedule"),
        },
      );
    },
    [updateScheduleMut],
  );

  if (isLoading || !workflow) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
          <Skeleton className="h-5 w-60" />
        </div>
        <div className="p-6 space-y-4">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
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
        <AppLink href={p.workflows()} className="text-sm text-muted-foreground hover:text-foreground transition-colors">
          Workflows
        </AppLink>
        <ChevronRight className="h-3 w-3 text-muted-foreground" />
        {editingName ? (
          <input
            className="text-sm font-medium rounded border bg-transparent px-2 py-0.5 outline-none focus:ring-1 focus:ring-ring"
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") setEditingName(false);
            }}
            autoFocus
          />
        ) : (
          <span
            className="text-sm font-medium cursor-pointer hover:underline"
            onDoubleClick={() => { setEditName(workflow.name); setEditingName(true); }}
            title="Double-click to rename"
          >
            {workflow.name}
          </span>
        )}
      </div>

      {/* Content */}
      <div className="flex flex-1 min-h-0 overflow-hidden">
        {/* Left: Steps Editor */}
        <div className="flex-1 overflow-y-auto border-r p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">Steps</h2>
            <div className="flex gap-2">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button size="sm" variant="outline" className="h-7 gap-1.5" />
                  }
                >
                  <Plus className="h-3.5 w-3.5" />
                  Add Step
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => setAddStepType("agent")}>
                    <Bot className="h-4 w-4 mr-2" /> Agent Step
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setAddStepType("review")}>
                    <ShieldCheck className="h-4 w-4 mr-2" /> Review Step
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setAddStepType("approval")}>
                    <ShieldCheck className="h-4 w-4 mr-2" /> Approval Step
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button size="sm" className="h-7 gap-1.5" onClick={handleTrigger}>
                <Play className="h-3.5 w-3.5" />
                Run
              </Button>
            </div>
          </div>

          {workflow.steps.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
              <p className="text-sm">No steps yet. Add agent or approval steps to build your workflow.</p>
            </div>
          ) : (
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleReorder}>
              <SortableContext items={stepIds} strategy={verticalListSortingStrategy}>
                <div className="space-y-2">
                  {workflow.steps.map((step, i) => (
                    <StepCard
                      key={stepIds[i]}
                      step={step}
                      index={i}
                      agents={agents}
                      onRemove={() => handleRemoveStep(i)}
                      sortableId={stepIds[i]!}
                      showArrow={i > 0}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
          )}
        </div>

        {/* Right: Runs & Schedules */}
        <div className="w-80 shrink-0 flex flex-col overflow-hidden">
          {/* Tab header */}
          <div className="flex h-11 items-center border-b">
            <button
              type="button"
              className={cn(
                "flex-1 h-full text-sm font-medium transition-colors border-b-2",
                rightTab === "runs" ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              onClick={() => setRightTab("runs")}
            >
              Runs <span className="text-xs text-muted-foreground ml-1">{runs.length}</span>
            </button>
            <button
              type="button"
              className={cn(
                "flex-1 h-full text-sm font-medium transition-colors border-b-2",
                rightTab === "schedules" ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              onClick={() => setRightTab("schedules")}
            >
              Schedules <span className="text-xs text-muted-foreground ml-1">{schedules.length}</span>
            </button>
          </div>

          {rightTab === "runs" ? (
            <>
              {selectedRun ? (
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                  <div className="flex items-center justify-between">
                    <button
                      type="button"
                      className="text-xs text-muted-foreground hover:text-foreground"
                      onClick={() => setSelectedRunId(null)}
                    >
                      ← Back to runs
                    </button>
                    {(selectedRun.status === "running" || selectedRun.status === "paused") && (
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-6 text-xs"
                        onClick={() => cancelRun.mutate(selectedRun.id)}
                      >
                        Cancel
                      </Button>
                    )}
                  </div>

                  {selectedRun.step_runs.map((sr) => (
                    <div key={sr.id}>
                      <StepRunStatus stepRun={sr} />
                      {sr.step_type === "approval" && sr.status === "paused" && (
                        <div className="flex gap-2 mt-2 pl-11">
                          <Button size="sm" variant="outline" className="h-7 text-xs text-green-600" onClick={() => handleApproval(sr.id, "approved")}>Approve</Button>
                          <Button size="sm" variant="outline" className="h-7 text-xs text-yellow-600" onClick={() => handleApproval(sr.id, "rejected")}>Reject</Button>
                          <Button size="sm" variant="outline" className="h-7 text-xs text-red-600" onClick={() => handleApproval(sr.id, "stopped")}>Stop</Button>
                        </div>
                      )}
                      {sr.step_type === "review" && sr.status === "paused" && (
                        <div className="flex gap-2 mt-2 pl-11">
                          <Button size="sm" variant="outline" className="h-7 text-xs text-green-600" onClick={() => handleReview(sr.id, "approved")}>Approve</Button>
                          <Button size="sm" variant="outline" className="h-7 text-xs text-yellow-600" onClick={() => handleReview(sr.id, "rejected")}>Reject</Button>
                          <Button size="sm" variant="outline" className="h-7 text-xs text-red-600" onClick={() => handleReview(sr.id, "stopped")}>Stop</Button>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="flex-1 overflow-y-auto">
                  {runs.length === 0 ? (
                    <div className="flex flex-col items-center justify-center gap-1 py-12 text-muted-foreground">
                      <p className="text-xs">No runs yet</p>
                    </div>
                  ) : (
                    runs.map((run) => (
                      <RunRow key={run.id} run={run} onSelect={setSelectedRunId} />
                    ))
                  )}
                </div>
              )}
            </>
          ) : (
            /* Schedules tab */
            <div className="flex-1 overflow-y-auto">
              <div className="flex items-center justify-between px-4 py-2">
                <span className="text-xs text-muted-foreground">
                  {schedules.length} schedule{schedules.length !== 1 ? "s" : ""}
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  className="h-6 gap-1 text-xs"
                  onClick={() => setScheduleDialogOpen(true)}
                >
                  <Plus className="h-3 w-3" />
                  New
                </Button>
              </div>
              {schedules.length === 0 ? (
                <div className="flex flex-col items-center justify-center gap-1 py-12 text-muted-foreground">
                  <Clock className="h-8 w-8 text-muted-foreground/40" />
                  <p className="text-xs">No schedules</p>
                  <p className="text-[11px]">Add a schedule to trigger this workflow automatically.</p>
                </div>
              ) : (
                <div className="px-3 space-y-2 pb-3">
                  {schedules.map((s) => (
                    <ScheduleRow
                      key={s.id}
                      schedule={s}
                      onToggle={handleToggleSchedule}
                      onDelete={handleDeleteSchedule}
                      onRename={handleRenameSchedule}
                    />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Add Step Dialog */}
      <Dialog open={addStepType !== null} onOpenChange={(open) => { if (!open) setAddStepType(null); }}>
        <DialogContent className="sm:max-w-md">
          <DialogTitle>
            Add {addStepType === "agent" ? "Agent" : addStepType === "review" ? "Review" : "Approval"} Step
          </DialogTitle>
          <div className="space-y-3 pt-2">
            {addStepType === "agent" && (
              <>
                <div>
                  <label className="text-sm font-medium">Agent</label>
                  <select
                    className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    value={stepAgentId}
                    onChange={(e) => setStepAgentId(e.target.value)}
                  >
                    <option value="">Select an agent...</option>
                    {agents
                      .filter((a) => !a.archived_at)
                      .map((a) => (
                        <option key={a.id} value={a.id}>
                          {a.name}
                        </option>
                      ))}
                  </select>
                </div>
                <div>
                  <label className="text-sm font-medium">Prompt</label>
                  <textarea
                    className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-none"
                    placeholder="Instructions for this agent step..."
                    rows={3}
                    value={stepPrompt}
                    onChange={(e) => setStepPrompt(e.target.value)}
                  />
                </div>
              </>
            )}
            {addStepType === "review" && (
              <>
                <div>
                  <label className="text-sm font-medium">Reviewer Type</label>
                  <select
                    className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                    value={reviewerType}
                    onChange={(e) => setReviewerType(e.target.value as "agent" | "member")}
                  >
                    <option value="agent">AI Agent</option>
                    <option value="member">Human Member</option>
                  </select>
                </div>
                {reviewerType === "agent" && (
                  <div>
                    <label className="text-sm font-medium">Review Agent</label>
                    <select
                      className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                      value={reviewAgentId}
                      onChange={(e) => setReviewAgentId(e.target.value)}
                    >
                      <option value="">Select a review agent...</option>
                      {agents
                        .filter((a) => !a.archived_at)
                        .map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.name}
                          </option>
                        ))}
                    </select>
                  </div>
                )}
                <div>
                  <label className="text-sm font-medium">Review Prompt</label>
                  <textarea
                    className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-none"
                    placeholder="Instructions for the reviewer..."
                    rows={3}
                    value={reviewPrompt}
                    onChange={(e) => setReviewPrompt(e.target.value)}
                  />
                </div>
              </>
            )}
            {addStepType === "approval" && (
              <p className="text-sm text-muted-foreground">
                An approval step pauses the workflow and waits for a reviewer to approve, reject, or stop the run.
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setAddStepType(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleAddStep}
                disabled={
                  (addStepType === "agent" && !stepAgentId) ||
                  (addStepType === "review" && reviewerType === "agent" && !reviewAgentId)
                }
              >
                Add Step
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Create Schedule Dialog */}
      <Dialog open={scheduleDialogOpen} onOpenChange={setScheduleDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogTitle>New Schedule</DialogTitle>
          <div className="space-y-3 pt-2">
            <div>
              <label className="text-sm font-medium">Name</label>
              <input
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                placeholder="e.g. Daily at 9am"
                value={scheduleName}
                onChange={(e) => setScheduleName(e.target.value)}
                autoFocus
              />
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
                          scheduleWeekdays.includes(day)
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
                    value={scheduleTimeOfDay}
                    onChange={(e) => setScheduleTimeOfDay(e.target.value)}
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
                  value={scheduleOnceAt}
                  onChange={(e) => setScheduleOnceAt(e.target.value)}
                />
              </div>
            )}

            <div>
              <label className="text-sm font-medium">Description</label>
              <textarea
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring resize-none"
                placeholder="Optional description..."
                rows={2}
                value={scheduleDesc}
                onChange={(e) => setScheduleDesc(e.target.value)}
              />
            </div>

            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => { setScheduleDialogOpen(false); resetScheduleForm(); }}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleCreateSchedule}
                disabled={!scheduleName.trim()}
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
