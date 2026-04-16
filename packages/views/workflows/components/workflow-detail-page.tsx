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
} from "lucide-react";
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, useSortable, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useQuery } from "@tanstack/react-query";
import { workflowDetailOptions, workflowRunsOptions, workflowRunDetailOptions, pendingApprovalsOptions } from "@multica/core/workflows/queries";
import { useUpdateWorkflow, useTriggerWorkflow, useCancelWorkflowRun, useApproveStepRun, useSubmitReview } from "@multica/core/workflows/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspaceStore } from "@multica/core/workspace";
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
import type { WorkflowStep, WorkflowStepType, WorkflowRun, WorkflowRunDetail, WorkflowStepRun, ApprovalDecision, ReviewDecision } from "@multica/core/types";

const RUN_STATUS_CONFIG: Record<string, { icon: React.ElementType; color: string; label: string }> = {
  pending: { icon: Clock, color: "text-muted-foreground", label: "Pending" },
  running: { icon: Loader2, color: "text-blue-500", label: "Running" },
  paused: { icon: PauseCircle, color: "text-yellow-500", label: "Awaiting Approval" },
  planning: { icon: Loader2, color: "text-purple-500", label: "Planning" },
  completed: { icon: CheckCircle2, color: "text-green-500", label: "Completed" },
  failed: { icon: XCircle, color: "text-red-500", label: "Failed" },
  cancelled: { icon: XCircle, color: "text-muted-foreground", label: "Cancelled" },
};

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

  return (
    <button
      type="button"
      className="flex h-10 w-full items-center gap-3 px-3 text-sm hover:bg-accent/40 transition-colors text-left"
      onClick={() => onSelect(run.id)}
    >
      <Icon className={cn("h-4 w-4 shrink-0", cfg.color, run.status === "running" && "animate-spin")} />
      <span className="flex-1 truncate">{cfg.label}</span>
      <span className="text-xs text-muted-foreground shrink-0">
        {new Date(run.created_at).toLocaleString()}
      </span>
    </button>
  );
}

function StepRunStatus({ stepRun }: { stepRun: WorkflowStepRun }) {
  const cfg = RUN_STATUS_CONFIG[stepRun.status] ?? RUN_STATUS_CONFIG.pending!;
  const Icon = cfg.icon;

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
          <Icon className={cn("h-3.5 w-3.5", cfg.color, stepRun.status === "running" && "animate-spin")} />
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
    </div>
  );
}

export function WorkflowDetailPage({ workflowId }: { workflowId: string }) {
  const wsId = useWorkspaceId();
  const workspace = useWorkspaceStore((s) => s.workspace);
  const { data: workflow, isLoading } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runs = [] } = useQuery(workflowRunsOptions(wsId, workflowId));
  const updateWorkflow = useUpdateWorkflow();
  const triggerMutation = useTriggerWorkflow();
  const cancelRun = useCancelWorkflowRun();
  const approveStep = useApproveStepRun();
  const submitReview = useSubmitReview();

  const [addStepType, setAddStepType] = useState<WorkflowStepType | null>(null);
  const [stepAgentId, setStepAgentId] = useState("");
  const [stepPrompt, setStepPrompt] = useState("");
  const [reviewAgentId, setReviewAgentId] = useState("");
  const [reviewPrompt, setReviewPrompt] = useState("");
  const [reviewerType, setReviewerType] = useState<"agent" | "member">("agent");
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState(false);
  const [editName, setEditName] = useState("");

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
        <AppLink href="/workflows" className="text-sm text-muted-foreground hover:text-foreground transition-colors">
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

        {/* Right: Runs */}
        <div className="w-80 shrink-0 flex flex-col overflow-hidden">
          <div className="flex h-11 items-center px-4 border-b">
            <span className="text-sm font-medium">Runs</span>
            <span className="ml-auto text-xs text-muted-foreground">{runs.length}</span>
          </div>

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
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-green-600"
                        onClick={() => handleApproval(sr.id, "approved")}
                      >
                        Approve
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-yellow-600"
                        onClick={() => handleApproval(sr.id, "rejected")}
                      >
                        Reject
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-red-600"
                        onClick={() => handleApproval(sr.id, "stopped")}
                      >
                        Stop
                      </Button>
                    </div>
                  )}
                  {sr.step_type === "review" && sr.status === "paused" && (
                    <div className="flex gap-2 mt-2 pl-11">
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-green-600"
                        onClick={() => handleReview(sr.id, "approved")}
                      >
                        Approve
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-yellow-600"
                        onClick={() => handleReview(sr.id, "rejected")}
                      >
                        Reject
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-7 text-xs text-red-600"
                        onClick={() => handleReview(sr.id, "stopped")}
                      >
                        Stop
                      </Button>
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
    </div>
  );
}
