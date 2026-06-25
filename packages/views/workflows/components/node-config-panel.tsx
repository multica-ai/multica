"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Trash2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateStage, useDeleteNode, useAssignNodeToStage } from "@multica/core/workflows/queries";
import { useWorkflowEditorStore } from "@multica/core/workflows/store";
import { AssigneePicker } from "../../issues/components/pickers/assignee-picker";
import type { WorkflowNode, WorkflowStage, WorkerType, CriticType } from "@multica/core/types";
import type { IssueAssigneeType } from "@multica/core/types/issue";

function toAssigneeType(t: string): IssueAssigneeType | null {
  if (t === "human") return "member";
  if (t === "agent" || t === "squad") return t as IssueAssigneeType;
  return null;
}

function fromAssigneeType(t: IssueAssigneeType | null): WorkerType {
  if (t === "member") return "human";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "human";
}

function fromAssigneeTypeCritic(t: IssueAssigneeType | null): CriticType {
  if (t === "member") return "human";
  if (t === "agent") return "agent";
  if (t === "squad") return "squad";
  return "human";
}

function toFormatSchemaString(fs: unknown): string {
  if (!fs) return "";
  if (typeof fs === "string") return fs;
  return JSON.stringify(fs, null, 2);
}

/**
 * Parse a format_schema textarea value back to a JSON value for API storage.
 * The textarea always holds a string, but format_schema must be stored as a
 * JSON object/array/null in the database (JSONB column), not as a JSON string.
 * A bare string causes validateJSONSchema in Go to fail with
 * "cannot unmarshal string into Go value of type map[string]interface{}".
 */
function parseFormatSchemaValue(raw: string): unknown {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    // JSON is incomplete while typing — keep the raw string so the edit
    // is preserved; handleSave normalizes it again before sending to the API.
    return raw;
  }
}

interface NodeConfigPanelProps {
  node: WorkflowNode;
  workflowId: string;
  nodes?: WorkflowNode[];
  stages?: WorkflowStage[];
  disabled?: boolean;
  onClose: () => void;
}

export function NodeConfigPanel({ node, workflowId, nodes = [], stages = [], disabled = false, onClose }: NodeConfigPanelProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const deleteMutation = useDeleteNode(wsId, workflowId);
  const assignStageMutation = useAssignNodeToStage(wsId, workflowId);
  const createStageMutation = useCreateStage(wsId, workflowId);
  const nodeEdits = useWorkflowEditorStore((s) => s.nodeEdits);
  const cacheNodeEdits = useWorkflowEditorStore((s) => s.cacheNodeEdits);

  const saved = nodeEdits[node.id];

  const isAnnotation = (() => {
    const fs = saved?.format_schema ?? node.format_schema;
    return Boolean(
      fs &&
      typeof fs === "object" &&
      !Array.isArray(fs) &&
      (fs as Record<string, unknown>).type === "annotation",
    );
  })();

  const [title, setTitle] = useState(saved?.title ?? node.title);
  const [description, setDescription] = useState(saved?.description ?? node.description);
  const [formatSchema, setFormatSchema] = useState<string>(
    toFormatSchemaString(saved?.format_schema ?? node.format_schema)
  );
  const [workerType, setWorkerType] = useState(saved?.worker_type ?? node.worker_type);
  const [workerId, setWorkerId] = useState<string | null>(saved?.worker_id ?? node.worker_id ?? null);
  const [criticType, setCriticType] = useState(saved?.critic_type ?? node.critic_type);
  const [criticId, setCriticId] = useState<string | null>(saved?.critic_id ?? node.critic_id ?? null);
  const [criticApiUrl, setCriticApiUrl] = useState(saved?.critic_api_url ?? node.critic_api_url ?? "");
  const [stageId, setStageId] = useState<string | null>(node.stage_id ?? null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [newStageName, setNewStageName] = useState("");
  const [newStageDescription, setNewStageDescription] = useState("");

  useEffect(() => {
    setStageId(node.stage_id ?? null);
  }, [node.stage_id]);

  const bindableNodes = useMemo(
    () => nodes.filter((n) => {
      if (n.id === node.id) return false;
      const fs = n.format_schema;
      return !(fs && typeof fs === "object" && !Array.isArray(fs) && (fs as Record<string, unknown>).type === "annotation");
    }),
    [nodes, node.id],
  );

  const getTargetNodeId = (): string | null => {
    const fs = nodeEdits[node.id]?.format_schema ?? node.format_schema;
    if (fs && typeof fs === "object" && !Array.isArray(fs)) {
      return (fs as Record<string, unknown>).annotation_target_node_id as string | null ?? null;
    }
    return null;
  };

  const targetNodeId = getTargetNodeId();

  useEffect(() => {
    const s = nodeEdits[node.id];
    setTitle(s?.title ?? node.title);
    setDescription(s?.description ?? node.description);
    setFormatSchema(toFormatSchemaString(s?.format_schema ?? node.format_schema));
    setWorkerType(s?.worker_type ?? node.worker_type);
    setWorkerId(s?.worker_id ?? node.worker_id ?? null);
    setCriticType(s?.critic_type ?? node.critic_type);
    setCriticId(s?.critic_id ?? node.critic_id ?? null);
    setCriticApiUrl(s?.critic_api_url ?? node.critic_api_url ?? "");
  }, [node.id]);

  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(node.id);
      toast.success(t(($) => $.node.toast_deleted));
      onClose();
    } catch {
      toast.error(t(($) => $.node.toast_delete_failed));
    }
  };

  return (
    <div className="flex flex-col h-full border-l bg-card">
      <div className="flex items-center justify-between px-4 py-3 border-b shrink-0">
        <h3 className="text-sm font-medium">{t(($) => $.node.title)}</h3>
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
          <svg width="15" height="15" viewBox="0 0 15 15" fill="none">
            <path d="M11.7816 4.03157C12.0062 3.80702 12.0062 3.44295 11.7816 3.2184C11.5571 2.99385 11.193 2.99385 10.9685 3.2184L7.50005 6.68682L4.03164 3.2184C3.80708 2.99385 3.44301 2.99385 3.21846 3.2184C2.99391 3.44295 2.99391 3.80702 3.21846 4.03157L6.68688 7.49999L3.21846 10.9684C2.99391 11.193 2.99391 11.557 3.21846 11.7816C3.44301 12.0061 3.80708 12.0061 4.03164 11.7816L7.50005 8.31316L10.9685 11.7816C11.193 12.0061 11.5571 12.0061 11.7816 11.7816C12.0062 11.557 12.0062 11.193 11.7816 10.9684L8.31322 7.49999L11.7816 4.03157Z" fill="currentColor" />
          </svg>
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 min-h-0">
        <div className="space-y-4">
        {/* Title */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.title)}</Label>
          <Input disabled={disabled}
            value={title}
            onChange={(e) => { setTitle(e.target.value); cacheNodeEdits(node.id, { title: e.target.value }); }}
            placeholder={t(($) => $.node.title_placeholder)}
            className="h-8 text-sm"
          />
        </div>

        {/* Description */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.description)}</Label>
          <Textarea disabled={disabled}
            value={description}
            onChange={(e) => { setDescription(e.target.value); cacheNodeEdits(node.id, { description: e.target.value }); }}
            placeholder={t(($) => $.node.description_placeholder)}
            className="min-h-[60px] text-sm"
            rows={2}
          />
        </div>

        {/* Stage assignment — immediate mutation, not batched with other edits */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.stage_label)}</Label>
          <select
            disabled={disabled || assignStageMutation.isPending}
            className="flex h-8 w-full rounded-md border border-input bg-background px-2 text-sm"
            value={stageId ?? ""}
            onChange={(e) => {
              const newVal = e.target.value;
              if (newVal === "__create_new__") {
                // Reset the select visually — the sentinel value shouldn't linger
                (e.target as HTMLSelectElement).value = stageId ?? "";
                setShowCreateForm(true);
                return;
              }
              const newStageId = newVal || null;
              setStageId(newStageId);
              assignStageMutation.mutate(
                { nodeId: node.id, stage_id: newStageId },
                { onError: () => setStageId(node.stage_id ?? null) },
              );
            }}
          >
            <option value="">{t(($) => $.overview.stage_canvas.unassigned)}</option>
            {stages.map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
            <option value="__create_new__" disabled={disabled}>
              {t(($) => $.node.stage_create_option)}
            </option>
          </select>

          {showCreateForm && (
            <div className="space-y-2 rounded-md border border-muted p-3">
              <Input
                disabled={disabled}
                value={newStageName}
                onChange={(e) => setNewStageName(e.target.value)}
                placeholder={t(($) => $.node.stage_create_name_placeholder)}
                className="h-8 text-sm"
                autoFocus
              />
              <Input
                disabled={disabled}
                value={newStageDescription}
                onChange={(e) => setNewStageDescription(e.target.value)}
                placeholder={t(($) => $.node.stage_create_description_placeholder)}
                className="h-8 text-sm"
              />
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="default"
                  disabled={disabled || !newStageName.trim() || createStageMutation.isPending}
                  onClick={async () => {
                    if (!newStageName.trim()) return;
                    try {
                      const created = await createStageMutation.mutateAsync({
                        name: newStageName.trim(),
                        description: newStageDescription.trim() || undefined,
                      });
                      // Auto-select the newly created stage for this node
                      setStageId(created.id);
                      assignStageMutation.mutate(
                        { nodeId: node.id, stage_id: created.id },
                        { onError: () => setStageId(node.stage_id ?? null) },
                      );
                      setShowCreateForm(false);
                      setNewStageName("");
                      setNewStageDescription("");
                    } catch {
                      // Error captured by mutation state and displayed below
                    }
                  }}
                >
                  {createStageMutation.isPending ? t(($) => $.node.saving) : t(($) => $.detail.create_dialog.create)}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={createStageMutation.isPending}
                  onClick={() => {
                    setShowCreateForm(false);
                    setNewStageName("");
                    setNewStageDescription("");
                  }}
                >
                  {t(($) => $.overview.stage_dialog.cancel)}
                </Button>
              </div>
              {createStageMutation.error && (
                <p className="text-xs text-destructive">{createStageMutation.error.message}</p>
              )}
            </div>
          )}
        </div>

        {/* Format Schema */}
        <div className="space-y-1.5">
          <Label className="text-sm">{t(($) => $.node.format_schema_label)}</Label>
          <Textarea disabled={disabled}
            value={formatSchema}
            onChange={(e) => {
              setFormatSchema(e.target.value);
              cacheNodeEdits(node.id, { format_schema: parseFormatSchemaValue(e.target.value) });
            }}
            placeholder="{}"
            className="min-h-[80px] text-sm font-mono"
            rows={4}
          />
          <p className="text-[11px] text-muted-foreground">{t(($) => $.node.format_schema_hint)}</p>
        </div>

        {/* Bind to Node — only for annotations */}
        {isAnnotation && (
        <div className="space-y-1.5">
          <Label className="text-sm">Bind to Node</Label>
          {targetNodeId ? (
            <div className="flex items-center gap-1.5">
              <span className="text-sm flex-1 truncate">
                {bindableNodes.find((bn) => bn.id === targetNodeId)?.title ?? "Unknown node"}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 shrink-0"
                onClick={() => {
                  let obj: Record<string, unknown> = {};
                  try {
                    const parsed = JSON.parse(formatSchema || "{}");
                    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
                      obj = parsed as Record<string, unknown>;
                    }
                  } catch { /* keep default */ }
                  delete obj.annotation_target_node_id;
                  cacheNodeEdits(node.id, { format_schema: obj });
                  setFormatSchema(JSON.stringify(obj, null, 2));
                }}
                title="Unbind"
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
          ) : (
            <select disabled={disabled}
              className="flex h-8 w-full rounded-md border border-input bg-background px-2 text-sm"
              value=""
              onChange={(e) => {
                const tid = e.target.value;
                if (!tid) return;
                let obj: Record<string, unknown> = {};
                try {
                  const parsed = JSON.parse(formatSchema || "{}");
                  if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
                    obj = parsed as Record<string, unknown>;
                  }
                } catch { /* keep default */ }
                obj.annotation_target_node_id = tid;
                cacheNodeEdits(node.id, { format_schema: obj });
                setFormatSchema(JSON.stringify(obj, null, 2));
              }}
            >
              <option value="">Select a node...</option>
              {bindableNodes.map((bn) => (
                <option key={bn.id} value={bn.id}>{bn.title}</option>
              ))}
            </select>
          )}
        </div>
        )}


        {/* Worker config — hidden for annotations */}
        {!isAnnotation && (
        <div className="space-y-3 pt-2 border-t">
          <h4 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {t(($) => $.node.section_worker)}
          </h4>

          <div className="space-y-1.5">
            <div className={disabled ? "pointer-events-none" : undefined} onClickCapture={disabled ? (e) => { e.preventDefault(); e.stopPropagation(); } : undefined}>
            <AssigneePicker
                assigneeType={toAssigneeType(workerType)}
                assigneeId={workerId}
                onUpdate={disabled ? () => {} : (u) => {
                  const wt = fromAssigneeType(u.assignee_type ?? null);
                  const wid = u.assignee_id ?? null;
                  setWorkerType(wt);
                  setWorkerId(wid);
                  cacheNodeEdits(node.id, { worker_type: wt, worker_id: wid });
                }}
                align="start"
                skipBuiltinRuntimeSelection
              />
            </div>
          </div>

        </div>
        )}

        {/* Critic config — hidden for annotations */}
        {!isAnnotation && (
        <div className="space-y-3 pt-2 border-t">
          <h4 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider">
            {t(($) => $.node.section_critic)}
          </h4>

          <div className="space-y-1.5">
              <div className={disabled ? "pointer-events-none" : undefined} onClickCapture={disabled ? (e) => { e.preventDefault(); e.stopPropagation(); } : undefined}>
              <AssigneePicker
                assigneeType={toAssigneeType(criticType)}
                assigneeId={criticId}
                onUpdate={disabled ? () => {} : (u) => {
                  const ct = fromAssigneeTypeCritic(u.assignee_type ?? null);
                  const cid = u.assignee_id ?? null;
                  setCriticType(ct);
                  setCriticId(cid);
                  cacheNodeEdits(node.id, { critic_type: ct, critic_id: cid });
                }}
                align="start"
              />
            </div>
          </div>

          {criticType === "api" && (
            <div className="space-y-1.5">
              <Label className="text-sm">{t(($) => $.node.critic_api_url_label)}</Label>
              <Input disabled={disabled}
                value={criticApiUrl}
                onChange={(e) => { setCriticApiUrl(e.target.value); cacheNodeEdits(node.id, { critic_api_url: e.target.value }); }}
                placeholder="https://..."
                className="h-8 text-sm"
              />
              <p className="text-[11px] text-muted-foreground">{t(($) => $.node.critic_api_url_hint)}</p>
            </div>
          )}

        </div>
        )}
        </div>
      </div>

      {!disabled && (
      <div className="px-4 py-3 border-t shrink-0">
          <Button
            size="sm"
            variant="destructive"
            className="w-full"
            onClick={handleDelete}
            disabled={deleteMutation.isPending}
          >
            <Trash2 className="h-3.5 w-3.5 mr-1.5" />
            {deleteMutation.isPending ? t(($) => $.node.saving) : t(($) => $.node.delete)}
          </Button>
      </div>
      )}
    </div>
  );
}
