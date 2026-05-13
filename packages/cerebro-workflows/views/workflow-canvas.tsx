"use client";

import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  type Edge,
  type Node,
  type NodeProps,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";

import {
  ACTION_OPTIONS,
  TRIGGER_OPTIONS,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  createWorkflow,
  updateWorkflow,
} from "../core";
import type {
  CerebroWorkflowActionType,
  CerebroWorkflowTriggerType,
  WorkflowWriteInput,
} from "../core/types";
import { EMPTY_FORM, buildPayload, formStateFromInput, type FormState } from "./workflow-form";

const ISSUE_STATUSES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

const TRIGGER_NODE_ID = "trigger";
const ACTION_NODE_ID = "action";

type CanvasNodeKind = "trigger" | "action";

interface CanvasNodeData extends Record<string, unknown> {
  kind: CanvasNodeKind;
}

const DEFAULT_NODES: Node<CanvasNodeData>[] = [
  {
    id: TRIGGER_NODE_ID,
    type: "trigger",
    position: { x: 80, y: 80 },
    data: { kind: "trigger" },
  },
  {
    id: ACTION_NODE_ID,
    type: "action",
    position: { x: 80, y: 320 },
    data: { kind: "action" },
  },
];

const DEFAULT_EDGES: Edge[] = [
  { id: "trigger-action", source: TRIGGER_NODE_ID, target: ACTION_NODE_ID },
];

export interface WorkflowCanvasProps {
  workflowId?: string;
  headerSlot?: ReactNode;
  embedded?: boolean;
}

// Node-canvas workflow builder (JEH-1103 phase 2). Renders trigger + action as
// xyflow nodes connected by a single edge. Conditions live on a sub-panel of
// the trigger node (kept implicit in phase 2 — multi-condition chains land
// when we add a dedicated condition trigger type).
export function WorkflowCanvas(props: WorkflowCanvasProps) {
  return (
    <ReactFlowProvider>
      <WorkflowCanvasInner {...props} />
    </ReactFlowProvider>
  );
}

function WorkflowCanvasInner({ workflowId, headerSlot, embedded }: WorkflowCanvasProps) {
  const featureEnabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();

  const wsId = workspace?.id ?? "";

  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });

  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [selected, setSelected] = useState<CanvasNodeKind>("trigger");
  const [error, setError] = useState<string | null>(null);
  const [nodes, setNodes, onNodesChange] = useNodesState<Node<CanvasNodeData>>(DEFAULT_NODES);
  const [edges, , onEdgesChange] = useEdgesState<Edge>(DEFAULT_EDGES);
  const hydrated = useRef(false);

  useEffect(() => {
    if (!detail.data || hydrated.current) return;
    hydrated.current = true;
    const w = detail.data;
    setForm(
      formStateFromInput({
        name: w.name,
        enabled: w.enabled,
        trigger_type: w.trigger_type,
        trigger_config: w.trigger_config,
        action_type: w.action_type,
        action_config: w.action_config,
      }),
    );
    const layout = parseLayout(w.editor_layout);
    if (layout && layout.length > 0) {
      setNodes((curr) =>
        curr.map((n) => {
          const saved = layout.find((s) => s.id === n.id);
          return saved ? { ...n, position: saved.position } : n;
        }),
      );
    }
  }, [detail.data, setNodes]);

  const save = useMutation({
    mutationFn: async () => {
      const payload: WorkflowWriteInput = {
        ...buildPayload(form),
        editor_mode: "canvas",
        editor_layout: {
          nodes: nodes.map((n) => ({ id: n.id, position: n.position })),
        },
      };
      if (workflowId) return updateWorkflow(workflowId, payload);
      return createWorkflow(payload);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
      if (workspace) navigation.push(`/${workspace.slug}/workflows`);
    },
    onError: (err: unknown) => {
      setError(err instanceof Error ? err.message : "Kunne ikke gemme workflow");
    },
  });

  const nodeTypes = useMemo<NodeTypes>(
    () => ({
      trigger: TriggerCanvasNode,
      action: ActionCanvasNode,
    }),
    [],
  );

  const onNodeClick = useCallback((_: unknown, node: Node<CanvasNodeData>) => {
    setSelected((node.data?.kind as CanvasNodeKind | undefined) ?? "trigger");
  }, []);

  if (!featureEnabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const body = (
    <div className="flex flex-1 min-h-0">
      <div className="relative flex-1 min-w-0">
        <ReactFlow
          nodes={nodes.map((n) => ({
            ...n,
            data: {
              ...n.data,
              label:
                n.data.kind === "trigger"
                  ? triggerLabel(form.triggerType)
                  : actionLabel(form.actionType),
              selected: selected === n.data.kind,
            },
          }))}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          nodesDraggable
          nodesConnectable={false}
          edgesFocusable={false}
          fitView
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={16} />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
      <aside className="flex w-[360px] shrink-0 flex-col border-l bg-card">
        <div className="flex-1 min-h-0 overflow-y-auto p-4">
          <Inspector
            selected={selected}
            form={form}
            setForm={setForm}
            statuses={ISSUE_STATUSES}
          />
        </div>
        {error && (
          <div className="mx-4 mb-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive">
            {error}
          </div>
        )}
        <div className="flex items-center justify-end gap-2 border-t p-3">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigation.push(`/${workspace.slug}/workflows`)}
          >
            Annullér
          </Button>
          <Button
            type="button"
            disabled={save.isPending}
            onClick={() => {
              setError(null);
              save.mutate();
            }}
            data-testid="canvas-save"
          >
            {save.isPending ? "Gemmer…" : "Gem"}
          </Button>
        </div>
      </aside>
    </div>
  );

  if (embedded) return body;

  const heading = workflowId ? "Rediger workflow" : "Nyt workflow";
  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">{heading}</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            Trigger → action på canvas. Klik en node for at redigere felter til højre.
          </p>
        </div>
        {headerSlot}
      </PageHeader>
      {body}
    </div>
  );
}

// Inspector renders the field set for the currently-selected node. The
// trigger inspector also owns the workflow-level Name/Enabled fields so the
// user doesn't need a separate "workflow settings" panel — they belong
// conceptually to the trigger side.
function Inspector({
  selected,
  form,
  setForm,
  statuses,
}: {
  selected: CanvasNodeKind;
  form: FormState;
  setForm: (f: FormState) => void;
  statuses: string[];
}) {
  if (selected === "trigger") {
    return (
      <div className="flex flex-col gap-4">
        <Field label="Workflow-navn">
          <Input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            required
            placeholder='F.eks. "Når in_review → opret QA sub-issue"'
          />
        </Field>
        <Field label="Aktiv">
          <Switch
            checked={form.enabled}
            onCheckedChange={(v) => setForm({ ...form, enabled: v === true })}
          />
        </Field>
        <Field label="Trigger-type">
          <NativeSelect
            value={form.triggerType}
            onChange={(e) =>
              setForm({ ...form, triggerType: e.target.value as CerebroWorkflowTriggerType })
            }
            data-testid="canvas-trigger-type"
          >
            {TRIGGER_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </NativeSelect>
        </Field>
        {form.triggerType === "status_changed" && (
          <>
            <Field label="Fra status (tom = alle)">
              <NativeSelect
                value={form.fromStatus}
                onChange={(e) => setForm({ ...form, fromStatus: e.target.value })}
              >
                <option value="">(alle)</option>
                {statuses.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <Field label="Til status">
              <NativeSelect
                value={form.toStatus}
                onChange={(e) => setForm({ ...form, toStatus: e.target.value })}
              >
                {statuses.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          </>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Field label="Action-type">
        <NativeSelect
          value={form.actionType}
          onChange={(e) =>
            setForm({ ...form, actionType: e.target.value as CerebroWorkflowActionType })
          }
          data-testid="canvas-action-type"
        >
          {ACTION_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </NativeSelect>
      </Field>

      {form.actionType === "set_status" && (
        <Field label="Sæt status til">
          <NativeSelect
            value={form.setStatus}
            onChange={(e) => setForm({ ...form, setStatus: e.target.value })}
          >
            {statuses.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </NativeSelect>
        </Field>
      )}

      {form.actionType === "create_sub_issue" && (
        <>
          <Field label="Sub-issue titel">
            <Input
              value={form.subIssueTitle}
              onChange={(e) => setForm({ ...form, subIssueTitle: e.target.value })}
              required
              placeholder="F.eks. QA: {{title}}"
            />
          </Field>
          <Field label="Beskrivelse (valgfri)">
            <Textarea
              value={form.subIssueDescription}
              onChange={(e) => setForm({ ...form, subIssueDescription: e.target.value })}
              rows={3}
            />
          </Field>
          <Field label="Assignee-type">
            <NativeSelect
              value={form.subIssueAssigneeType}
              onChange={(e) =>
                setForm({
                  ...form,
                  subIssueAssigneeType: e.target.value as "member" | "agent",
                })
              }
            >
              <option value="agent">Agent</option>
              <option value="member">Member</option>
            </NativeSelect>
          </Field>
          <Field label="Assignee-ID (valgfri)">
            <Input
              value={form.subIssueAssigneeId}
              onChange={(e) => setForm({ ...form, subIssueAssigneeId: e.target.value })}
              placeholder="<uuid>"
            />
          </Field>
          <Field label="Label-IDs (komma-separeret, valgfri)">
            <Input
              value={form.subIssueLabelIDs}
              onChange={(e) => setForm({ ...form, subIssueLabelIDs: e.target.value })}
              placeholder="<uuid>,<uuid>"
            />
          </Field>
        </>
      )}

      {form.actionType === "send_reminder" && (
        <>
          <Field label="Modtagertype">
            <NativeSelect
              value={form.reminderRecipientType}
              onChange={(e) =>
                setForm({
                  ...form,
                  reminderRecipientType: e.target.value as "member" | "agent",
                })
              }
            >
              <option value="member">Member</option>
              <option value="agent">Agent</option>
            </NativeSelect>
          </Field>
          <Field label="Modtager-ID">
            <Input
              value={form.reminderRecipientId}
              onChange={(e) => setForm({ ...form, reminderRecipientId: e.target.value })}
              required
              placeholder="<uuid>"
            />
          </Field>
          <Field label="Besked">
            <Textarea
              value={form.reminderMessage}
              onChange={(e) => setForm({ ...form, reminderMessage: e.target.value })}
              rows={3}
              required
            />
          </Field>
        </>
      )}

      {form.actionType === "run_skill" && (
        <>
          <Field label="Skill (navn)">
            <Input
              value={form.skillName}
              onChange={(e) => setForm({ ...form, skillName: e.target.value })}
              required
              placeholder="firtal-data-evaluate"
              data-testid="canvas-skill-name"
            />
          </Field>
          <Field label="Agent-ID">
            <Input
              value={form.skillAgentId}
              onChange={(e) => setForm({ ...form, skillAgentId: e.target.value })}
              required
              placeholder="<uuid>"
              data-testid="canvas-skill-agent"
            />
          </Field>
          <Field label="Skill-input (JSON)">
            <Textarea
              value={form.skillInputJSON}
              onChange={(e) => setForm({ ...form, skillInputJSON: e.target.value })}
              rows={5}
              placeholder='{"query": "..."}'
              data-testid="canvas-skill-input"
            />
          </Field>
        </>
      )}

      {form.actionType === "comment_on_issue" && (
        <>
          <Field label="Target">
            <NativeSelect
              value={form.commentTarget}
              onChange={(e) =>
                setForm({ ...form, commentTarget: e.target.value as "self" | "parent" })
              }
            >
              <option value="self">Triggerede issue</option>
              <option value="parent">Parent-issue</option>
            </NativeSelect>
          </Field>
          <Field label="Indhold">
            <Textarea
              value={form.commentContent}
              onChange={(e) => setForm({ ...form, commentContent: e.target.value })}
              rows={5}
              required
              placeholder="Opdatering på {{title}}: ..."
            />
          </Field>
        </>
      )}

      {form.actionType === "route_by_domain" && (
        <>
          <Field label="Label-præfiks">
            <Input
              value={form.routeLabelPrefix}
              onChange={(e) =>
                setForm({ ...form, routeLabelPrefix: e.target.value })
              }
              placeholder="domain:"
              data-testid="canvas-route-prefix"
            />
          </Field>
          <Field label="Default-domæne">
            <NativeSelect
              value={form.routeDefaultDomain}
              onChange={(e) =>
                setForm({
                  ...form,
                  routeDefaultDomain: e.target
                    .value as FormState["routeDefaultDomain"],
                })
              }
              data-testid="canvas-route-default"
            >
              <option value="code">code</option>
              <option value="business">business</option>
              <option value="design">design</option>
              <option value="content">content</option>
            </NativeSelect>
          </Field>
          <p className="text-[11px] text-muted-foreground">
            Krav: workspacet har et label pr. domæne (fx{" "}
            <code>domain:code</code>). Heuristikken klassificerer ud fra
            titel + beskrivelse.
          </p>
        </>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

function TriggerCanvasNode({ data, selected }: NodeProps<Node<CanvasNodeData & {
  label?: string;
  selected?: boolean;
}>>) {
  const isActive = selected || data.selected === true;
  return (
    <div
      className={cls(
        "rounded-md border bg-card px-4 py-3 text-sm shadow-sm",
        isActive ? "border-primary ring-2 ring-primary/30" : "border-border",
      )}
    >
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Trigger</div>
      <div className="text-sm font-medium">{data.label ?? "Status changed"}</div>
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}

function ActionCanvasNode({ data, selected }: NodeProps<Node<CanvasNodeData & {
  label?: string;
  selected?: boolean;
}>>) {
  const isActive = selected || data.selected === true;
  return (
    <div
      className={cls(
        "rounded-md border bg-card px-4 py-3 text-sm shadow-sm",
        isActive ? "border-primary ring-2 ring-primary/30" : "border-border",
      )}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">Action</div>
      <div className="text-sm font-medium">{data.label ?? "Create sub-issue"}</div>
    </div>
  );
}

function cls(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

function triggerLabel(t: CerebroWorkflowTriggerType): string {
  const opt = TRIGGER_OPTIONS.find((o) => o.value === t);
  return opt?.label ?? t;
}

function actionLabel(a: CerebroWorkflowActionType): string {
  const opt = ACTION_OPTIONS.find((o) => o.value === a);
  return opt?.label ?? a;
}

interface SavedNodePosition {
  id: string;
  position: { x: number; y: number };
}

// parseLayout reads stored editor_layout JSON and returns a list of (id,
// position) pairs we can apply to the live nodes. Any malformed entry is
// dropped — we never reject the whole layout because of one bad row.
function parseLayout(raw: unknown): SavedNodePosition[] | null {
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as { nodes?: unknown };
  if (!Array.isArray(obj.nodes)) return null;
  const out: SavedNodePosition[] = [];
  for (const entry of obj.nodes) {
    if (!entry || typeof entry !== "object") continue;
    const e = entry as { id?: unknown; position?: unknown };
    if (typeof e.id !== "string") continue;
    if (!e.position || typeof e.position !== "object") continue;
    const p = e.position as { x?: unknown; y?: unknown };
    if (typeof p.x !== "number" || typeof p.y !== "number") continue;
    out.push({ id: e.id, position: { x: p.x, y: p.y } });
  }
  return out;
}
