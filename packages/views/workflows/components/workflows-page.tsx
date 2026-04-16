"use client";

import { useState, useCallback } from "react";
import { Plus, Workflow as WorkflowIcon, ChevronRight, Play, Trash2, Pencil } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workflowListOptions } from "@multica/core/workflows/queries";
import { useCreateWorkflow, useDeleteWorkflow, useTriggerWorkflow, useUpdateWorkflow } from "@multica/core/workflows/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths, useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { WorkspaceAvatar } from "../../workspace/workspace-avatar";
import { AppLink } from "../../navigation";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { toast } from "sonner";
import type { Workflow, WorkflowMode } from "@multica/core/types";

function WorkflowRow({
  workflow,
  onTrigger,
  onDelete,
  onRename,
}: {
  workflow: Workflow;
  onTrigger: (id: string) => void;
  onDelete: (id: string) => void;
  onRename: (id: string, name: string) => void;
}) {
  const agentSteps = workflow.steps.filter((s) => s.type === "agent").length;
  const approvalSteps = workflow.steps.filter((s) => s.type === "approval").length;
  const reviewSteps = workflow.steps.filter((s) => s.type === "review").length;
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(workflow.name);
  const p = useWorkspacePaths();

  const commitRename = useCallback(() => {
    const trimmed = editName.trim();
    if (trimmed && trimmed !== workflow.name) {
      onRename(workflow.id, trimmed);
    }
    setEditing(false);
  }, [editName, workflow.name, workflow.id, onRename]);

  return (
    <div className="group/row flex h-12 items-center gap-3 px-5 text-sm transition-colors hover:bg-accent/40 border-b border-border/40">
      {editing ? (
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <WorkflowIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
          <input
            className="min-w-0 flex-1 rounded border bg-transparent px-2 py-1 text-sm font-medium outline-none focus:ring-1 focus:ring-ring"
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename();
              if (e.key === "Escape") { setEditName(workflow.name); setEditing(false); }
            }}
            autoFocus
          />
        </div>
      ) : (
        <AppLink
          href={p.workflowDetail(workflow.id)}
          className="flex min-w-0 flex-1 items-center gap-3"
        >
          <WorkflowIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate font-medium">{workflow.name}</span>
          <span className="text-xs text-muted-foreground shrink-0">
            {workflow.mode === "orchestrated" && (
              <span className="text-purple-500 mr-2">orchestrated</span>
            )}
            {agentSteps} agent{agentSteps !== 1 ? "s" : ""}
            {approvalSteps > 0 && `, ${approvalSteps} approval${approvalSteps !== 1 ? "s" : ""}`}
            {reviewSteps > 0 && `, ${reviewSteps} review${reviewSteps !== 1 ? "s" : ""}`}
          </span>
        </AppLink>
      )}
      <div className="flex items-center gap-1 opacity-0 group-hover/row:opacity-100 transition-opacity">
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={() => { setEditName(workflow.name); setEditing(true); }}
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={() => onTrigger(workflow.id)}
        >
          <Play className="h-3.5 w-3.5" />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-destructive"
          onClick={() => onDelete(workflow.id)}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}

export function WorkflowsPage() {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const { data: workflows = [], isLoading } = useQuery(workflowListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const createWorkflow = useCreateWorkflow();
  const deleteWorkflow = useDeleteWorkflow();
  const triggerWorkflow = useTriggerWorkflow();
  const updateWorkflow = useUpdateWorkflow();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [mode, setMode] = useState<WorkflowMode>("sequential");
  const [plannerAgentId, setPlannerAgentId] = useState("");

  const handleCreate = useCallback(() => {
    if (!name.trim()) return;
    createWorkflow.mutate(
      {
        name: name.trim(),
        description: description.trim(),
        steps: [],
        mode,
        planner_agent_id: mode === "orchestrated" ? plannerAgentId || undefined : undefined,
      },
      {
        onSuccess: () => {
          setDialogOpen(false);
          setName("");
          setDescription("");
          setMode("sequential");
          setPlannerAgentId("");
          toast.success("Workflow created");
        },
        onError: () => toast.error("Failed to create workflow"),
      },
    );
  }, [name, description, mode, plannerAgentId, createWorkflow]);

  const handleTrigger = useCallback(
    (id: string) => {
      triggerWorkflow.mutate(id, {
        onSuccess: () => toast.success("Workflow triggered"),
        onError: () => toast.error("Failed to trigger workflow"),
      });
    },
    [triggerWorkflow],
  );

  const handleDelete = useCallback(
    (id: string) => {
      deleteWorkflow.mutate(id, {
        onSuccess: () => toast.success("Workflow deleted"),
        onError: () => toast.error("Failed to delete workflow"),
      });
    },
    [deleteWorkflow],
  );

  const handleRename = useCallback(
    (id: string, name: string) => {
      updateWorkflow.mutate(
        { id, name },
        {
          onSuccess: () => toast.success("Workflow renamed"),
          onError: () => toast.error("Failed to rename workflow"),
        },
      );
    },
    [updateWorkflow],
  );

  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-1.5 border-b px-4">
          <Skeleton className="h-5 w-40" />
        </div>
        <div className="p-4 space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
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
        <span className="text-sm font-medium">Workflows</span>
      </div>

      {/* Header */}
      <div className="flex h-11 shrink-0 items-center justify-between border-b px-4">
        <span className="text-sm text-muted-foreground">{workflows.length} workflow{workflows.length !== 1 ? "s" : ""}</span>
        <Button size="sm" variant="outline" className="h-7 gap-1.5" onClick={() => setDialogOpen(true)}>
          <Plus className="h-3.5 w-3.5" />
          New Workflow
        </Button>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto">
        {workflows.length === 0 ? (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground pt-20">
            <WorkflowIcon className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">No workflows yet</p>
            <p className="text-xs">Create a workflow to orchestrate multi-agent tasks.</p>
          </div>
        ) : (
          workflows.map((wf) => (
            <WorkflowRow
              key={wf.id}
              workflow={wf}
              onTrigger={handleTrigger}
              onDelete={handleDelete}
              onRename={handleRename}
            />
          ))
        )}
      </div>

      {/* Create Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogTitle>New Workflow</DialogTitle>
          <div className="space-y-3 pt-2">
            <div>
              <label className="text-sm font-medium">Name</label>
              <input
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                placeholder="e.g. Code Review Pipeline"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleCreate();
                }}
              />
            </div>
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
            <div>
              <label className="text-sm font-medium">Mode</label>
              <select
                className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                value={mode}
                onChange={(e) => setMode(e.target.value as WorkflowMode)}
              >
                <option value="sequential">Sequential — fixed step order</option>
                <option value="orchestrated">Orchestrated — CEO agent plans dynamically</option>
              </select>
            </div>
            {mode === "orchestrated" && (
              <div>
                <label className="text-sm font-medium">Planner Agent (CEO)</label>
                <select
                  className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
                  value={plannerAgentId}
                  onChange={(e) => setPlannerAgentId(e.target.value)}
                >
                  <option value="">Select a planner agent...</option>
                  {agents
                    .filter((a: { id: string; name: string; archived_at?: string | null }) => !a.archived_at)
                    .map((a: { id: string; name: string }) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                </select>
              </div>
            )}
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleCreate}
                disabled={!name.trim() || (mode === "orchestrated" && !plannerAgentId)}
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
