"use client";

// Cerebro workflow v2a (FIR-1550) — Workspace Settings → Statusmodeller.
//
// One place to: create/edit/delete reusable status models, assign one model
// per project (or fall back to Default), and see at a glance which projects
// run a custom workflow (the overview line at the top).

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  cerebroStatusModelsListOptions,
  cerebroStatusModelAssignmentsOptions,
  useCreateStatusModelMutation,
  useUpdateStatusModelMutation,
  useDeleteStatusModelMutation,
  useAssignProjectStatusModelMutation,
  useClearProjectStatusModelMutation,
} from "@multica/cerebro-status-models/core";
import type { CerebroStatusModel } from "@multica/cerebro-status-models/core";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { StatusModelEditor } from "./status-model-editor";

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
  const assignMutation = useAssignProjectStatusModelMutation(wsId);
  const clearMutation = useClearProjectStatusModelMutation(wsId);

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<CerebroStatusModel | undefined>();

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

  const handleDelete = (model: CerebroStatusModel) => {
    if (model.project_count > 0) {
      toast.error(
        `"${model.name}" bruges af ${model.project_count} projekt(er) — fjern den der først.`,
      );
      return;
    }
    deleteMutation.mutate(model.id, {
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : "Kunne ikke slette modellen."),
      onSuccess: () => toast.success(`"${model.name}" slettet.`),
    });
  };

  const handleAssign = (projectId: string, value: string) => {
    if (value === DEFAULT_VALUE) {
      clearMutation.mutate(projectId, {
        onError: () => toast.error("Kunne ikke nulstille projektets statusmodel."),
      });
      return;
    }
    assignMutation.mutate(
      { projectId, statusModelId: value },
      { onError: () => toast.error("Kunne ikke tildele statusmodellen.") },
    );
  };

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold">Statusmodeller</h2>
          <p className="text-sm text-muted-foreground">
            Genbrugelige status-pipelines. Vælg én pr. projekt — projektets board
            bruger så modellens statusser i stedet for standarderne.
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="mr-1.5 size-3.5" />
          Ny model
        </Button>
      </div>

      <div className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm">
        <GitBranch className="size-4 text-muted-foreground" />
        <span>
          <span className="font-medium">{projectsWithModel}</span> projekt(er)
          bruger et custom workflow ·{" "}
          <span className="font-medium">{models.length}</span> model(ler) defineret
        </span>
      </div>

      {/* Models list */}
      <div className="space-y-3">
        {models.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            Ingen statusmodeller endnu. Opret den første med "Ny model".
          </p>
        ) : (
          models.map((model) => (
            <div key={model.id} className="rounded-lg border p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{model.name}</span>
                    <Badge variant="secondary">
                      {model.project_count} projekt(er)
                    </Badge>
                  </div>
                  {model.description && (
                    <p className="mt-0.5 text-sm text-muted-foreground">
                      {model.description}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" size="icon-sm" onClick={() => openEdit(model)}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
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
        <h3 className="text-sm font-semibold">Projekter</h3>
        <p className="text-sm text-muted-foreground">
          Vælg hvilken statusmodel hvert projekt bruger. "Default" giver de 7
          standardstatusser.
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
              Ingen projekter i dette workspace endnu.
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
    </div>
  );
}
