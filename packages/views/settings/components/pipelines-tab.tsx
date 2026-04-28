"use client";

import { useState, useCallback } from "react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  GripVertical,
  Plus,
  Trash2,
  ChevronDown,
  ChevronRight,
  Star,
  Workflow,
  Pencil,
  Check,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Badge } from "@multica/ui/components/ui/badge";
import { Switch } from "@multica/ui/components/ui/switch";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { useQuery } from "@tanstack/react-query";
import {
  usePipelines,
  usePipelineColumns,
  useCreatePipeline,
  useUpdatePipeline,
  useDeletePipeline,
  useSetDefaultPipeline,
  useSyncPipelineColumns,
} from "@multica/core/pipeline";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Pipeline, PipelineColumnInput } from "@multica/core/types";

// ─── Types ────────────────────────────────────────────────────────────────────

interface LocalColumn extends PipelineColumnInput {
  _localId: string;
}

// ─── Main tab ─────────────────────────────────────────────────────────────────

export function PipelinesTab() {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: pipelines = [], isLoading } = usePipelines(wsId);
  const createPipeline = useCreatePipeline(wsId);
  const [createOpen, setCreateOpen] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";

  if (isLoading) {
    return <div className="text-sm text-muted-foreground py-8 text-center">Loading pipelines…</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold">Pipelines</h2>
          <p className="text-sm text-muted-foreground mt-0.5">
            Configure named column sets for your board. Custom status keys define workflow stages.
          </p>
        </div>
        {canManageWorkspace && (
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" />
            New pipeline
          </Button>
        )}
      </div>

      {!canManageWorkspace && (
        <p className="text-sm text-muted-foreground">
          Only admins and owners can manage pipelines.
        </p>
      )}

      {pipelines.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
            <Workflow className="size-8 text-muted-foreground" />
            <p className="text-sm font-medium">No pipelines yet</p>
            <p className="text-xs text-muted-foreground max-w-xs">
              Create a pipeline to define custom columns for your board with instructions and transition rules.
            </p>
            {canManageWorkspace && (
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                <Plus className="size-4" />
                Create pipeline
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {pipelines.map((pipeline) => (
            <PipelineRow
              key={pipeline.id}
              pipeline={pipeline}
              wsId={wsId}
              canManageWorkspace={canManageWorkspace}
              expanded={expandedId === pipeline.id}
              onToggleExpand={() =>
                setExpandedId((prev) => (prev === pipeline.id ? null : pipeline.id))
              }
            />
          ))}
        </div>
      )}

      {canManageWorkspace && createOpen && (
        <CreatePipelineDialog
          wsId={wsId}
          onClose={() => setCreateOpen(false)}
          onCreate={async (name, description) => {
            await createPipeline.mutateAsync({ name, description });
            setCreateOpen(false);
            toast.success("Pipeline created");
          }}
        />
      )}
    </div>
  );
}

// ─── Pipeline row ─────────────────────────────────────────────────────────────

function PipelineRow({
  pipeline,
  wsId,
  canManageWorkspace,
  expanded,
  onToggleExpand,
}: {
  pipeline: Pipeline;
  wsId: string;
  canManageWorkspace: boolean;
  expanded: boolean;
  onToggleExpand: () => void;
}) {
  const updatePipeline = useUpdatePipeline(wsId);
  const deletePipeline = useDeletePipeline(wsId);
  const setDefault = useSetDefaultPipeline(wsId);
  const [editMode, setEditMode] = useState(false);
  const [editName, setEditName] = useState(pipeline.name);
  const [editDesc, setEditDesc] = useState(pipeline.description);
  const [deleteOpen, setDeleteOpen] = useState(false);

  async function handleSaveEdit() {
    if (!editName.trim()) return;
    await updatePipeline.mutateAsync({ id: pipeline.id, name: editName.trim(), description: editDesc });
    setEditMode(false);
    toast.success("Pipeline updated");
  }

  function handleCancelEdit() {
    setEditName(pipeline.name);
    setEditDesc(pipeline.description);
    setEditMode(false);
  }

  return (
    <Card>
      <CardContent className="p-0">
        <div className="flex items-center gap-3 px-4 py-3">
          <button
            className="flex items-center gap-2 min-w-0 flex-1 text-left"
            onClick={onToggleExpand}
          >
            {expanded ? (
              <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
            ) : (
              <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
            )}
            {editMode ? (
              <div className="flex-1 space-y-1.5" onClick={(e) => e.stopPropagation()}>
                <Input
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  placeholder="Pipeline name"
                  className="h-7 text-sm"
                  autoFocus
                />
                <Input
                  value={editDesc}
                  onChange={(e) => setEditDesc(e.target.value)}
                  placeholder="Description (optional)"
                  className="h-7 text-sm"
                />
              </div>
            ) : (
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium truncate">{pipeline.name}</span>
                  {pipeline.is_default && (
                    <Badge variant="secondary" className="text-[10px] px-1.5 py-0 gap-1 shrink-0">
                      <Star className="size-2.5" />
                      Default
                    </Badge>
                  )}
                </div>
                {pipeline.description && (
                  <p className="text-xs text-muted-foreground truncate mt-0.5">{pipeline.description}</p>
                )}
              </div>
            )}
          </button>

          {canManageWorkspace && (
            <div className="flex items-center gap-1 shrink-0">
              {editMode ? (
                <>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={handleSaveEdit}
                    disabled={updatePipeline.isPending}
                  >
                    <Check className="size-3.5 text-success" />
                  </Button>
                  <Button variant="ghost" size="icon-sm" onClick={handleCancelEdit}>
                    <X className="size-3.5" />
                  </Button>
                </>
              ) : (
                <>
                  {!pipeline.is_default && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-xs h-7"
                      onClick={() =>
                        setDefault
                          .mutateAsync(pipeline.id)
                          .then(() => toast.success("Default pipeline updated"))
                      }
                      disabled={setDefault.isPending}
                    >
                      Set default
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setEditMode(true)}
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => setDeleteOpen(true)}
                    className="text-destructive hover:text-destructive"
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </>
              )}
            </div>
          )}
        </div>

        {expanded && (
          <div className="border-t px-4 py-4">
            <PipelineColumnsEditor wsId={wsId} pipelineId={pipeline.id} canManageWorkspace={canManageWorkspace} />
          </div>
        )}
      </CardContent>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete pipeline?</AlertDialogTitle>
            <AlertDialogDescription>
              &quot;{pipeline.name}&quot; and all its columns will be permanently deleted.
              {pipeline.is_default && (
                <span className="block mt-2 font-medium text-destructive">
                  This is your default pipeline. Deleting it will leave no default.
                </span>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() =>
                deletePipeline
                  .mutateAsync(pipeline.id)
                  .then(() => toast.success("Pipeline deleted"))
              }
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

// ─── Columns editor ───────────────────────────────────────────────────────────

function PipelineColumnsEditor({
  wsId,
  pipelineId,
  canManageWorkspace,
}: {
  wsId: string;
  pipelineId: string;
  canManageWorkspace: boolean;
}) {
  const { data: serverColumns = [], isLoading } = usePipelineColumns(wsId, pipelineId);
  const syncColumns = useSyncPipelineColumns(wsId, pipelineId);

  const [columns, setColumns] = useState<LocalColumn[] | null>(null);
  const [expandedColId, setExpandedColId] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  // Initialize local state from server once loaded
  const displayed: LocalColumn[] =
    columns ??
    serverColumns.map((c, i) => ({
      _localId: `${c.id}-${i}`,
      status_key: c.status_key,
      label: c.label,
      position: c.position,
      is_terminal: c.is_terminal,
      instructions: c.instructions,
      allowed_transitions: c.allowed_transitions,
    }));

  function markDirty(updated: LocalColumn[]) {
    setColumns(updated);
    setDirty(true);
  }

  function addColumn() {
    const nextPos = displayed.length > 0 ? Math.max(...displayed.map((c) => c.position)) + 1 : 0;
    const newCol: LocalColumn = {
      _localId: `new-${Date.now()}`,
      status_key: "",
      label: "",
      position: nextPos,
      is_terminal: false,
      instructions: "",
      allowed_transitions: [],
    };
    markDirty([...displayed, newCol]);
    setExpandedColId(newCol._localId);
  }

  function removeColumn(localId: string) {
    markDirty(
      displayed
        .filter((c) => c._localId !== localId)
        .map((c, i) => ({ ...c, position: i })),
    );
  }

  function updateColumn(localId: string, patch: Partial<LocalColumn>) {
    markDirty(displayed.map((c) => (c._localId === localId ? { ...c, ...patch } : c)));
  }

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const oldIdx = displayed.findIndex((c) => c._localId === active.id);
      const newIdx = displayed.findIndex((c) => c._localId === over.id);
      if (oldIdx === -1 || newIdx === -1) return;
      const reordered = arrayMove(displayed, oldIdx, newIdx).map((c, i) => ({
        ...c,
        position: i,
      }));
      markDirty(reordered);
    },
    [displayed],
  );

  async function handleSave() {
    const validation = validateColumns(displayed);
    if (validation) {
      toast.error(validation);
      return;
    }
    await syncColumns.mutateAsync(
      displayed.map(({ _localId: _l, ...col }) => col),
    );
    setColumns(null);
    setDirty(false);
    toast.success("Columns saved");
  }

  if (isLoading) {
    return <p className="text-xs text-muted-foreground py-4">Loading columns…</p>;
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Columns</p>
        {canManageWorkspace && (
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={addColumn}>
              <Plus className="size-3.5" />
              Add column
            </Button>
            {dirty && (
              <Button size="sm" onClick={handleSave} disabled={syncColumns.isPending}>
                {syncColumns.isPending ? "Saving…" : "Save columns"}
              </Button>
            )}
          </div>
        )}
      </div>

      {displayed.length === 0 ? (
        <p className="text-xs text-muted-foreground py-4 text-center">
          No columns yet. Add one to get started.
        </p>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={displayed.map((c) => c._localId)}
            strategy={verticalListSortingStrategy}
          >
            <div className="space-y-2">
              {displayed.map((col) => (
                <SortableColumnRow
                  key={col._localId}
                  col={col}
                  allColumns={displayed}
                  readOnly={!canManageWorkspace}
                  expanded={expandedColId === col._localId}
                  onToggleExpand={() =>
                    setExpandedColId((prev) =>
                      prev === col._localId ? null : col._localId,
                    )
                  }
                  onChange={(patch) => updateColumn(col._localId, patch)}
                  onRemove={() => removeColumn(col._localId)}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      )}
    </div>
  );
}

// ─── Sortable column row ──────────────────────────────────────────────────────

function SortableColumnRow({
  col,
  allColumns,
  readOnly,
  expanded,
  onToggleExpand,
  onChange,
  onRemove,
}: {
  col: LocalColumn;
  allColumns: LocalColumn[];
  readOnly: boolean;
  expanded: boolean;
  onToggleExpand: () => void;
  onChange: (patch: Partial<LocalColumn>) => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: col._localId,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const otherColumns = allColumns.filter((c) => c._localId !== col._localId && c.status_key);

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="rounded-lg border bg-card"
    >
      <div className="flex items-center gap-2 px-3 py-2.5">
        {!readOnly && (
          <button
            className="cursor-grab touch-none text-muted-foreground shrink-0"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="size-4" />
          </button>
        )}

        <div className="flex flex-1 gap-2 min-w-0">
          <Input
            value={col.status_key}
            onChange={(e) => onChange({ status_key: e.target.value.replace(/\s/g, "_") })}
            placeholder="status_key"
            className="h-7 text-xs font-mono w-36 shrink-0"
            disabled={readOnly}
          />
          <Input
            value={col.label}
            onChange={(e) => onChange({ label: e.target.value })}
            placeholder="Display label"
            className="h-7 text-sm flex-1 min-w-0"
            disabled={readOnly}
          />
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <button
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            onClick={onToggleExpand}
          >
            {expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            More
          </button>
          {!readOnly && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onRemove}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="size-3.5" />
            </Button>
          )}
        </div>
      </div>

      {expanded && (
        <div className="border-t px-3 py-3 space-y-3">
          <div className="flex items-center justify-between">
            <Label className="text-xs">Terminal column</Label>
            <Switch
              checked={col.is_terminal}
              onCheckedChange={(v) => onChange({ is_terminal: v })}
              disabled={readOnly}
            />
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">Instructions (markdown)</Label>
            <Textarea
              value={col.instructions}
              onChange={(e) => onChange({ instructions: e.target.value })}
              placeholder="Write guidance for agents working in this column…"
              className="text-sm min-h-[80px] resize-y"
              disabled={readOnly}
            />
          </div>

          {otherColumns.length > 0 && (
            <div className="space-y-1.5">
              <Label className="text-xs">Allowed transitions (output columns)</Label>
              <div className="flex flex-wrap gap-2">
                {otherColumns.map((other) => {
                  const checked = col.allowed_transitions.includes(other.status_key);
                  return (
                    <label
                      key={other._localId}
                      className={`flex items-center gap-1.5 ${readOnly ? "cursor-default" : "cursor-pointer"}`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => {
                          const next = e.target.checked
                            ? [...col.allowed_transitions, other.status_key]
                            : col.allowed_transitions.filter((t) => t !== other.status_key);
                          onChange({ allowed_transitions: next });
                        }}
                        className="accent-primary"
                        disabled={readOnly}
                      />
                      <span className="text-xs font-mono">{other.status_key}</span>
                      {other.label && (
                        <span className="text-xs text-muted-foreground">({other.label})</span>
                      )}
                    </label>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Create dialog ────────────────────────────────────────────────────────────

function CreatePipelineDialog({
  wsId: _wsId,
  onClose,
  onCreate,
}: {
  wsId: string;
  onClose: () => void;
  onCreate: (name: string, description: string) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    try {
      await onCreate(name.trim(), description);
    } finally {
      setSaving(false);
    }
  }

  return (
    <AlertDialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>New pipeline</AlertDialogTitle>
          <AlertDialogDescription>
            Give your pipeline a name. You can add columns after creating it.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <form onSubmit={handleSubmit} className="space-y-3 mt-2">
          <div className="space-y-1.5">
            <Label htmlFor="pipeline-name">Name</Label>
            <Input
              id="pipeline-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Engineering workflow"
              autoFocus
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pipeline-desc">Description (optional)</Label>
            <Input
              id="pipeline-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Short description"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel type="button" onClick={onClose}>Cancel</AlertDialogCancel>
            <AlertDialogAction type="submit" disabled={!name.trim() || saving}>
              {saving ? "Creating…" : "Create"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// ─── Validation ───────────────────────────────────────────────────────────────

function validateColumns(cols: LocalColumn[]): string | null {
  const keys = new Set<string>();
  for (const col of cols) {
    if (!col.status_key.trim()) return "All columns must have a status key.";
    if (/\s/.test(col.status_key)) return `status_key "${col.status_key}" cannot contain whitespace.`;
    if (!col.label.trim()) return `Column "${col.status_key}" must have a label.`;
    if (keys.has(col.status_key)) return `Duplicate status_key: "${col.status_key}".`;
    keys.add(col.status_key);
  }
  return null;
}
