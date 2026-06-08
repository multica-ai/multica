"use client";

// Cerebro workflow v2a (FIR-1550) — Workspace Settings → Statusmodeller.
//
// One place to: create/edit/delete reusable status models, assign one model
// per project (or fall back to Default), and see at a glance which projects
// run a custom workflow (the overview line at the top).

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Pencil, Plus, Star, StarOff, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  cerebroStatusModelsListOptions,
  cerebroStatusModelAssignmentsOptions,
  useCreateStatusModelMutation,
  useUpdateStatusModelMutation,
  useDeleteStatusModelMutation,
  useClearProjectStatusModelMutation,
  useSetWorkspaceDefaultMutation,
  useClearWorkspaceDefaultMutation,
} from "@multica/cerebro-status-models/core";
import type { CerebroStatusModel } from "@multica/cerebro-status-models/core";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { StatusModelEditor } from "./status-model-editor";
import { StatusModelAssignModal } from "./status-model-assign-modal";

const DEFAULT_VALUE = "__default__";

export function StatusModelsTab() {
  const wsId = useWorkspaceId();
  const { data: modelsData } = useQuery(cerebroStatusModelsListOptions(wsId));
  const { data: assignmentsData } = useQuery(
    cerebroStatusModelAssignmentsOptions(wsId),
  );
  const { data: projects } = useQuery(projectListOptions(wsId));

  const createMutation = useCreateStatusModelMutation(wsId);
  const updateMutation = useUpdateStatusModelMutation(wsId);
  const deleteMutation = useDeleteStatusModelMutation(wsId);
  // assignMutation is invoked inside StatusModelAssignModal — kept imported
  // for the modal child so the v2b mapping flow owns the assignment call.
  const clearMutation = useClearProjectStatusModelMutation(wsId);
  const setDefaultMutation = useSetWorkspaceDefaultMutation(wsId);
  const clearDefaultMutation = useClearWorkspaceDefaultMutation(wsId);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<CerebroStatusModel | undefined>();
  // v2b (FIR-1550): mapping-modal state — open per (project, model) selection.
  const [assigning, setAssigning] = useState<
    { projectId: string; projectName: string; modelId: string } | null
  >(null);

  const models = modelsData?.status_models ?? [];
  const assignments = assignmentsData?.assignments ?? [];

  const modelByProject = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of assignments) map.set(a.project_id, a.status_model_id);
    return map;
  }, [assignments]);

  const projectsWithModel = useMemo(
    () => assignments.filter((a) => models.some((m) => m.id === a.status_model_id)).length,
    [assignments, models],
  );

  const openCreate = () => {
    setEditing(undefined);
    setEditorOpen(true);
  };
  const openEdit = (model: CerebroStatusModel) => {
    setEditing(model);
    setEditorOpen(true);
  };

  const handleToggleDefault = (model: CerebroStatusModel) => {
    if (model.workspace_default) {
      clearDefaultMutation.mutate(undefined, {
        onError: () => toast.error("Could not remove the workspace default."),
      });
    } else {
      setDefaultMutation.mutate(model.id, {
        onError: () => toast.error("Could not set the workspace default."),
        onSuccess: () =>
          toast.success(`"${model.name}" is now the workspace standard — new projects will use it automatically.`),
      });
    }
  };

  const handleDelete = (model: CerebroStatusModel) => {
    if (model.project_count > 0) {
      toast.error(
        `"${model.name}" is used by ${model.project_count} project(s) — remove it from those first.`,
      );
      return;
    }
    deleteMutation.mutate(model.id, {
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : "Could not delete the model."),
      onSuccess: () => toast.success(`"${model.name}" deleted.`),
    });
  };

  const handleAssign = (projectId: string, value: string) => {
    if (value === DEFAULT_VALUE) {
      clearMutation.mutate(projectId, {
        onError: () => toast.error("Could not reset the project's status model."),
      });
      return;
    }
    // v2b (FIR-1550): no silent omplacering — open the mapping modal so the
    // admin sees per-base custom_status suggestions before the assignment
    // lands. The modal calls assignProjectStatusModel itself on confirm.
    const project = projects?.find((p) => p.id === projectId);
    setAssigning({
      projectId,
      projectName: project?.title ?? "project",
      modelId: value,
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">Status models</h2>
          <p className="text-sm text-muted-foreground">
            Reusable status pipelines. Pick one per project — the project's board
            then uses the model's statuses instead of the defaults.
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="mr-1.5 size-3.5" />
          New model
        </Button>
      </div>

      <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm">
        <GitBranch className="size-4 text-muted-foreground" />
        <span>
          <span className="font-medium">{projectsWithModel}</span> project(s) use
          a custom workflow ·{" "}
          <span className="font-medium">{models.length}</span> model(s) defined
        </span>
      </div>

      {/* Models list */}
      <div className="space-y-3">
        {models.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            No status models yet. Create the first one with "New model".
          </p>
        ) : (
          models.map((model) => (
            <div key={model.id} className="rounded-lg border p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{model.name}</span>
                    <Badge variant="secondary">
                      {model.project_count} project(s)
                    </Badge>
                    {model.workspace_default && (
                      <Badge variant="outline" className="text-xs border-amber-400 text-amber-600 bg-amber-50 dark:bg-amber-950/30 dark:text-amber-400">
                        Workspace standard
                      </Badge>
                    )}
                  </div>
                  {model.description && (
                    <p className="mt-0.5 text-sm text-muted-foreground">
                      {model.description}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title={model.workspace_default ? "Remove as workspace standard" : "Set as workspace standard"}
                    disabled={setDefaultMutation.isPending || clearDefaultMutation.isPending}
                    onClick={() => handleToggleDefault(model)}
                  >
                    {model.workspace_default ? (
                      <StarOff className="size-3.5 text-amber-500" />
                    ) : (
                      <Star className="size-3.5" />
                    )}
                  </Button>
                  <Button variant="ghost" size="icon-sm" title="Edit model" onClick={() => openEdit(model)}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="Delete model"
                    disabled={model.project_count > 0 || deleteMutation.isPending}
                    onClick={() => handleDelete(model)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </div>

              <div className="mt-2 flex flex-wrap gap-1.5">
                {[...model.statuses]
                  .sort((a, b) => a.position - b.position)
                  .map((s) => (
                    <span
                      key={s.key}
                      className="inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs"
                    >
                      <span
                        className="size-2 rounded-full"
                        style={{ backgroundColor: s.color || "var(--muted-foreground)" }}
                      />
                      {s.label}
                    </span>
                  ))}
              </div>
            </div>
          ))
        )}
      </div>

      {/* Per-project assignment + overview */}
      <div className="space-y-2">
        <h3 className="text-sm font-semibold">Projects</h3>
        <p className="text-sm text-muted-foreground">
          Choose which status model each project uses. "Default" gives the 7
          standard statuses.
        </p>
        <div className="divide-y rounded-lg border">
          {(projects ?? []).map((project) => (
            <div
              key={project.id}
              className="flex items-center justify-between gap-3 px-3 py-2"
            >
              <span className="min-w-0 truncate text-sm">{project.title}</span>
              <NativeSelect
                value={modelByProject.get(project.id) ?? DEFAULT_VALUE}
                onChange={(e) => handleAssign(project.id, e.target.value)}
                className="w-48 shrink-0"
              >
                <NativeSelectOption value={DEFAULT_VALUE}>Default</NativeSelectOption>
                {models.map((model) => (
                  <NativeSelectOption key={model.id} value={model.id}>
                    {model.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
          ))}
          {(projects ?? []).length === 0 && (
            <p className="px-3 py-4 text-center text-sm text-muted-foreground">
              No projects in this workspace yet.
            </p>
          )}
        </div>
      </div>

      {/* Mount only while open so the editor's initial state is re-seeded from
          `editing` on every open — create, edit, and switching models all get
          a fresh form instead of stale state from the previous session. */}
      {editorOpen && (
        <StatusModelEditor
          open={editorOpen}
          onOpenChange={setEditorOpen}
          model={editing}
          saving={createMutation.isPending || updateMutation.isPending}
          onSubmit={(input) =>
            editing
              ? updateMutation.mutateAsync({ id: editing.id, payload: input })
              : createMutation.mutateAsync(input)
          }
        />
      )}

      {assigning && (
        <StatusModelAssignModal
          open={!!assigning}
          projectId={assigning.projectId}
          projectName={assigning.projectName}
          modelId={assigning.modelId}
          onClose={() => setAssigning(null)}
        />
      )}
    </div>
  );
}
