"use client";

import { useMemo, useRef, useState } from "react";
import ELK from "elkjs/lib/elk.bundled.js";
import { Background, ConnectionMode, Controls, Handle, MarkerType, MiniMap, Position, ReactFlow, type Node, type NodeProps, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AlertTriangle, Bot, CheckCircle2, CircleStop, Copy, GitBranch, GitFork, ListTodo, Loader2, Play, Search, Settings2, Split, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { Agent, MemberWithUser } from "@multica/core/types";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { workflowAncestors, workflowTaskActor, workflowTaskActorPatch, workflowTaskMode, workflowTaskModePatch, type WorkflowAssignee, type WorkflowGraph, type WorkflowNode } from "@multica/core/workflows";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { matchesPinyin } from "../editor/extensions/pinyin-match";
import { PickerEmpty, PickerItem, PickerSection, PropertyPicker } from "../issues/components/pickers/property-picker";
import { useT } from "../i18n";

type CanvasNode = Node<{ node: WorkflowNode; actorName?: string; status?: string }, "workflow">;
const TASK_NODE_TYPES = ["agent", "human_task", "human_review", "condition"] as const;
const isTaskNode = (node: WorkflowNode) => TASK_NODE_TYPES.includes(node.type as (typeof TASK_NODE_TYPES)[number]);
const handleClassName = "!h-3 !w-3 !border-2 !border-background !bg-primary";

type WorkflowConnectionError = "missing" | "self" | "target_start" | "source_end" | "unknown_node" | "duplicate";

function workflowConnectionError(graph: WorkflowGraph, source: string | null, target: string | null): WorkflowConnectionError | null {
  if (!source || !target) return "missing";
  if (source === target) return "self";
  if (target === "start") return "target_start";
  if (source === "end") return "source_end";
  if (!graph.nodes.some((node) => node.id === source) || !graph.nodes.some((node) => node.id === target)) return "unknown_node";
  if (graph.edges.some((edge) => edge.source === source && edge.target === target)) return "duplicate";
  return null;
}

function canConnectWorkflowNodes(graph: WorkflowGraph, source: string | null, target: string | null): boolean {
  return workflowConnectionError(graph, source, target) === null;
}

function WorkflowActorPicker({
  agents,
  members,
  value,
  allowAgents,
  onSelect,
  disabled,
}: {
  agents: Agent[];
  members: MemberWithUser[];
  value?: WorkflowAssignee;
  allowAgents: boolean;
  onSelect: (actor: WorkflowAssignee | undefined) => void;
  disabled: boolean;
}) {
  const { t } = useT("workflows");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();
  const matches = (name: string) => !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const filteredMembers = members.filter((member) => matches(member.name));
  const filteredAgents = agents.filter((agent) => !agent.archived_at && matches(agent.name));
  const currentName = value?.type === "agent"
    ? agents.find((agent) => agent.id === value.id)?.name
    : members.find((member) => member.user_id === value?.id)?.name;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(next) => { setOpen(next); if (!next) setFilter(""); }}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.task_actor_search)}
      onSearchChange={setFilter}
      trigger={currentName ? <><ActorAvatar actorType={value!.type} actorId={value!.id} size="sm" /><span className="truncate">{currentName}</span></> : <span className="text-muted-foreground">{t(($) => $.task_actor_unassigned)}</span>}
      triggerRender={<Button variant="outline" className="w-full justify-start" disabled={disabled} />}
    >
      <PickerItem
        emptyValue
        selected={!value}
        onClick={() => { onSelect(undefined); setOpen(false); }}
      >
        <span className="text-muted-foreground">{t(($) => $.task_actor_unassigned)}</span>
      </PickerItem>
      {filteredMembers.length > 0 && (
        <PickerSection label={t(($) => $.task_actor_members)}>
          {filteredMembers.map((member) => (
            <PickerItem
              key={member.user_id}
              selected={value?.type === "member" && value.id === member.user_id}
              onClick={() => { onSelect({ type: "member", id: member.user_id }); setOpen(false); }}
            >
              <ActorAvatar actorType="member" actorId={member.user_id} size="sm" />
              <span className="truncate">{member.name}</span>
            </PickerItem>
          ))}
        </PickerSection>
      )}
      {allowAgents && filteredAgents.length > 0 && (
        <PickerSection label={t(($) => $.task_actor_agents)}>
          {filteredAgents.map((agent) => {
            const runtimeBound = isAgentRuntimeBound(agent);
            return (
              <PickerItem
                key={agent.id}
                selected={value?.type === "agent" && value.id === agent.id}
                disabled={!runtimeBound}
                tooltip={runtimeBound ? undefined : t(($) => $.task_actor_runtime_required)}
                onClick={() => { if (!runtimeBound) return; onSelect({ type: "agent", id: agent.id }); setOpen(false); }}
              >
                <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
                <span className="truncate">{agent.name}</span>
              </PickerItem>
            );
          })}
        </PickerSection>
      )}
      {filteredMembers.length === 0 && (!allowAgents || filteredAgents.length === 0) && <PickerEmpty />}
    </PropertyPicker>
  );
}

function WorkflowCanvasNode({ data, selected }: NodeProps<CanvasNode>) {
  const { t } = useT("workflows");
  const Icon = data.node.type === "start" ? Play : data.node.type === "end" ? CircleStop : data.node.type === "condition" ? GitFork : data.node.type === "parallel" ? Split : isTaskNode(data.node) ? ListTodo : Bot;
  const status = data.status?.toLowerCase();
  const StatusIcon = status === "running" ? Loader2 : status === "succeeded" ? CheckCircle2 : status === "failed" ? AlertTriangle : null;
  const statusTone = status === "running" ? "border-primary/70 shadow-primary/20 shadow-md" : status === "succeeded" ? "border-emerald-500/70" : status === "failed" ? "border-destructive/70" : status === "waiting_human" ? "border-amber-500/70" : "";
  return (
    <div className={cn("w-60 rounded-xl border bg-card p-4 text-card-foreground shadow-sm transition-colors", statusTone, selected && "border-primary ring-2 ring-primary/25")}>
      {data.node.type !== "start" && <Handle id="target" type="target" position={Position.Left} className={handleClassName} />}
      <div className="flex items-center gap-2"><Icon className="size-4 shrink-0 text-primary" /><strong className="truncate text-body">{data.node.label}</strong>{StatusIcon && <StatusIcon className={cn("ml-auto size-4 shrink-0", status === "running" && "animate-spin motion-reduce:animate-none text-primary", status === "succeeded" && "text-emerald-600", status === "failed" && "text-destructive")} aria-hidden="true" />}</div>
      {isTaskNode(data.node) && <p className="mt-2 truncate text-caption text-muted-foreground">{data.node.type === "condition" ? t(($) => $.task_condition) : data.actorName || t(($) => $.task_actor_unassigned)}</p>}
      {data.status && <p className={cn("mt-2 text-micro", status === "failed" ? "text-destructive" : status === "succeeded" ? "text-emerald-700" : "text-muted-foreground")} role="status">{t(($) => $.status[data.status as keyof typeof $.status])}</p>}
      {data.node.type !== "end" && <Handle id="source" type="source" position={Position.Right} className={handleClassName} />}
    </div>
  );
}
const nodeTypes = { workflow: WorkflowCanvasNode };

export function WorkflowCanvas({ graph, onChange, selectedNodeId, onSelect, agents, members, statuses, readOnly = false }: {
  graph: WorkflowGraph;
  onChange: (graph: WorkflowGraph) => void;
  selectedNodeId: string | null;
  onSelect: (id: string | null) => void;
  agents: Agent[];
  members: MemberWithUser[];
  statuses?: Record<string, string>;
  readOnly?: boolean;
}) {
  const { t } = useT("workflows");
  const [inspector, setInspector] = useState(false);
  const [layoutPending, setLayoutPending] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [connectionHint, setConnectionHint] = useState<string | null>(null);
  const graphRef = useRef(graph);
  graphRef.current = graph;
  const flowRef = useRef<ReactFlowInstance<CanvasNode> | null>(null);
  const selected = graph.nodes.find((node) => node.id === selectedNodeId);
  const ancestors = selected ? workflowAncestors(graph, selected.id) : [];
  const nodes = useMemo(() => graph.nodes.map((node): CanvasNode => {
    const actor = workflowTaskActor(node);
    return { id: node.id, type: "workflow", position: node.position, selected: node.id === selectedNodeId, data: { node, actorName: actor?.type === "agent" ? agents.find((agent) => agent.id === actor.id)?.name : members.find((member) => member.user_id === actor?.id)?.name, status: statuses?.[node.id] } };
  }), [graph.nodes, selectedNodeId, agents, members, statuses]);
  const edges = useMemo(() => graph.edges.map((edge) => {
    const kind = edge.kind ?? "flow";
    const label = edge.label ?? edge.sourcePort ?? (kind === "flow" ? undefined : kind);
    const active = statuses?.[edge.source] === "running" || statuses?.[edge.target] === "running";
    return {
      ...edge,
      type: "smoothstep" as const,
      markerEnd: { type: MarkerType.ArrowClosed },
      animated: active,
      label,
      labelShowBg: Boolean(label),
      labelBgPadding: [5, 2] as [number, number],
      labelBgBorderRadius: 4,
      labelBgStyle: { fill: "var(--background)", fillOpacity: 0.95 },
      labelStyle: { fontSize: 11, fontWeight: 600 },
      style: kind === "rework" ? { stroke: "var(--warning)", strokeDasharray: "6 4" } : kind === "compensation" ? { stroke: "var(--destructive)", strokeDasharray: "6 4" } : undefined,
    };
  }), [graph.edges, statuses]);
  const paletteItems = useMemo(() => [
    { type: "agent" as const, label: t(($) => $.add_agent), icon: Bot },
    { type: "human_task" as const, label: t(($) => $.add_human_task), icon: ListTodo },
    { type: "human_review" as const, label: t(($) => $.add_review), icon: CheckCircle2 },
    { type: "condition" as const, label: t(($) => $.add_condition), icon: GitFork },
    { type: "parallel" as const, label: t(($) => $.add_parallel), icon: Split },
  ], [t]);
  const visiblePaletteItems = paletteItems.filter((item) => !paletteQuery.trim() || item.label.toLowerCase().includes(paletteQuery.trim().toLowerCase()));
  function patchNode(patch: Partial<WorkflowNode>) {
    if (readOnly) return;
    if (selected) onChange({ ...graph, nodes: graph.nodes.map((node) => node.id === selected.id ? { ...node, ...patch } : node) });
  }
  function removeNodes(ids: Set<string>) {
    if (readOnly) return;
    const next = graphRef.current;
    const removable = new Set([...ids].filter((id) => id !== "start" && id !== "end"));
    if (!removable.size) return;
    onChange({ ...next, nodes: next.nodes.filter((node) => !removable.has(node.id)).map((node) => ({ ...node, inputRefs: node.inputRefs?.filter((id) => !removable.has(id)) })), edges: next.edges.filter((edge) => !removable.has(edge.source) && !removable.has(edge.target)) });
    if (selectedNodeId && removable.has(selectedNodeId)) { onSelect(null); setInspector(false); }
  }
  async function autoLayout() {
    if (readOnly) return;
    const source = graphRef.current;
    setLayoutPending(true);
    try {
      const result = await new ELK().layout({ id: "workflow", layoutOptions: { "elk.algorithm": "layered", "elk.direction": "RIGHT", "elk.spacing.nodeNode": "60", "elk.layered.spacing.nodeNodeBetweenLayers": "100" }, children: source.nodes.map((node) => ({ id: node.id, width: 240, height: 110 })), edges: source.edges.map((edge) => ({ id: edge.id, sources: [edge.source], targets: [edge.target] })) });
      // Layout must not overwrite edits made while the worker is running.
      if (graphRef.current !== source) return;
      const positions = new Map(result.children?.map((node) => [node.id, { x: node.x ?? 0, y: node.y ?? 0 }]));
      onChange({ ...source, nodes: source.nodes.map((node) => ({ ...node, position: positions.get(node.id) ?? node.position })) });
      requestAnimationFrame(() => flowRef.current?.fitView({ padding: 0.2 }));
    } catch { toast.error(t(($) => $.error)); } finally { setLayoutPending(false); }
  }
  function addNode(type: WorkflowNode["type"], label: string, extra: Partial<WorkflowNode> = {}) {
    if (readOnly) return;
    const id = crypto.randomUUID();
    const node: WorkflowNode = { id, type, label, position: { x: (selected?.position.x ?? 40) + 320, y: selected?.position.y ?? 160 }, ...extra };
    onChange({ ...graph, nodes: [...graph.nodes, node] });
    onSelect(id);
    setInspector(true);
  }
  function duplicateSelectedNode() {
    if (readOnly || !selected || selected.type === "start" || selected.type === "end") return;
    const id = crypto.randomUUID();
    onChange({ ...graph, nodes: [...graph.nodes, { ...selected, id, label: `${selected.label} (${t(($) => $.copy_suffix)})`, position: { x: selected.position.x + 40, y: selected.position.y + 150 } }] });
    onSelect(id);
  }
  function reportConnectionError(error: WorkflowConnectionError | null) {
    const message = error === "self" ? t(($) => $.connection_self) : error === "duplicate" ? t(($) => $.connection_duplicate) : error === "target_start" ? t(($) => $.connection_target_start) : error === "source_end" ? t(($) => $.connection_source_end) : t(($) => $.connection_invalid);
    setConnectionHint(message);
    toast.error(message);
  }
  function handleCanvasKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    const tagName = (event.target as HTMLElement | null)?.tagName;
    if (tagName === "INPUT" || tagName === "TEXTAREA" || tagName === "SELECT") return;
    if (event.key === "Escape") {
      setPaletteOpen(false);
      setPaletteQuery("");
      setConnectionHint(null);
      return;
    }
    if (readOnly) return;
    const modifier = event.metaKey || event.ctrlKey;
    if (event.key.toLowerCase() === "n" && !modifier) {
      event.preventDefault();
      setPaletteOpen((open) => !open);
      return;
    }
    if (modifier && event.key.toLowerCase() === "d") {
      event.preventDefault();
      duplicateSelectedNode();
      return;
    }
    if ((event.key === "Delete" || event.key === "Backspace") && selected) {
      event.preventDefault();
      removeNodes(new Set([selected.id]));
    }
  }
  return (
    <div className="relative h-full min-h-0 min-w-0 flex-1" aria-label={t(($) => $.title)} tabIndex={0} onKeyDown={handleCanvasKeyDown}>
      <div className="absolute left-3 top-3 z-10 flex max-w-[calc(100%-1.5rem)] flex-wrap gap-1 rounded-xl border bg-background/95 p-1.5 shadow-sm">
        <Button size="sm" variant={paletteOpen ? "secondary" : "ghost"} disabled={readOnly} aria-expanded={paletteOpen} aria-controls="workflow-node-palette" onClick={() => { setPaletteOpen((open) => !open); setPaletteQuery(""); }}><Search className="size-4" />{t(($) => $.node_library)}</Button>
        <Button size="sm" variant="ghost" disabled={readOnly} onClick={() => {
          const id = crypto.randomUUID();
          const node: WorkflowNode = { id, type: "agent", label: t(($) => $.task), position: { x: (selected?.position.x ?? 40) + 320, y: selected?.position.y ?? 160 }, instructions: "", inputRefs: [] };
          onChange({ ...graph, nodes: [...graph.nodes, node] }); onSelect(id); setInspector(true);
        }}><ListTodo className="size-4" />{t(($) => $.add_task)}</Button>
        <Button size="sm" variant="ghost" disabled={readOnly} onClick={() => addNode("parallel", t(($) => $.parallel))}><Split className="size-4" />{t(($) => $.add_parallel)}</Button>
        <Button size="sm" variant="ghost" disabled={readOnly || layoutPending} onClick={autoLayout}><GitBranch className="size-4" />{t(($) => $.layout)}</Button>
        <Button size="icon-sm" variant="ghost" aria-label={t(($) => $.configure)} disabled={readOnly || !selected} onClick={() => setInspector(true)}><Settings2 className="size-4" /></Button>
        <Button size="icon-sm" variant="ghost" aria-label={t(($) => $.duplicate)} disabled={readOnly || !selected || selected.type === "start" || selected.type === "end"} onClick={duplicateSelectedNode}><Copy className="size-4" /></Button>
        <Button size="icon-sm" variant="ghost" aria-label={t(($) => $.delete)} disabled={readOnly || !selected || selected.type === "start" || selected.type === "end"} onClick={() => selected && removeNodes(new Set([selected.id]))}><Trash2 className="size-4" /></Button>
      </div>
      {paletteOpen && <div id="workflow-node-palette" className="absolute left-3 top-14 z-20 w-72 rounded-xl border bg-background/95 p-2 shadow-lg" role="dialog" aria-label={t(($) => $.node_library)}>
        <div className="relative"><Search className="pointer-events-none absolute left-2 top-2.5 size-4 text-muted-foreground" /><Input autoFocus value={paletteQuery} onChange={(event) => setPaletteQuery(event.target.value)} placeholder={t(($) => $.node_library_search)} aria-label={t(($) => $.node_library_search)} className="pl-8" /></div>
        <div className="mt-2 grid gap-1" role="listbox" aria-label={t(($) => $.node_library)}>
          {visiblePaletteItems.map((item) => <Button key={item.type} type="button" variant="ghost" className="justify-start" onClick={() => { addNode(item.type, item.label); setPaletteOpen(false); setPaletteQuery(""); }}><item.icon className="size-4" />{item.label}</Button>)}
          {visiblePaletteItems.length === 0 && <p className="px-2 py-3 text-caption text-muted-foreground">{t(($) => $.node_library_empty)}</p>}
        </div>
        <p className="mt-2 border-t px-2 pt-2 text-micro text-muted-foreground">{t(($) => $.node_library_shortcut)}</p>
      </div>}
      <ReactFlow<CanvasNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} onInit={(instance) => { flowRef.current = instance; }} fitView minZoom={0.15} maxZoom={2} deleteKeyCode={readOnly ? [] : ["Backspace", "Delete"]} nodesDraggable={!readOnly} nodesConnectable={!readOnly} connectionMode={ConnectionMode.Strict} connectionRadius={32}
        isValidConnection={(connection) => !readOnly && canConnectWorkflowNodes(graphRef.current, connection.source, connection.target)}
        onNodeClick={(_, node) => onSelect(node.id)} onNodeDoubleClick={(_, node) => { onSelect(node.id); setInspector(true); }} onPaneClick={() => onSelect(null)}
        onNodesChange={(changes) => {
          const positions = changes.filter((change): change is Extract<typeof change, { type: "position" }> => change.type === "position" && !!change.position);
          if (readOnly || !positions.length) return;
          const next = graphRef.current;
          onChange({ ...next, nodes: next.nodes.map((node) => { const change = positions.find((item) => item.id === node.id); return change?.type === "position" && change.position ? { ...node, position: change.position } : node; }) });
        }}
        onNodesDelete={(deleted) => removeNodes(new Set(deleted.map((node) => node.id)))}
        onEdgesDelete={(deleted) => { if (readOnly) return; const ids = new Set(deleted.map((edge) => edge.id)); const next = graphRef.current; onChange({ ...next, edges: next.edges.filter((edge) => !ids.has(edge.id)) }); }}
        onConnect={({ source, target }) => {
          const error = workflowConnectionError(graphRef.current, source, target);
          if (readOnly || error) {
            reportConnectionError(error);
            return;
          }
          setConnectionHint(null);
          const next = graphRef.current;
          onChange({ ...next, edges: [...next.edges, { id: crypto.randomUUID(), source, target, kind: "flow" }] });
        }}
        onConnectEnd={(_, connectionState) => {
          if (readOnly || !("isValid" in connectionState) || connectionState.isValid !== false || !connectionState.fromNode) return;
          reportConnectionError(workflowConnectionError(graphRef.current, connectionState.fromNode.id, connectionState.toNode?.id ?? null));
        }}
      ><Background /><Controls /><MiniMap pannable zoomable nodeColor={(node) => {
        const nodeStatus = String((node.data as { status?: unknown } | undefined)?.status ?? "").toLowerCase();
        return nodeStatus === "running" ? "var(--primary)" : nodeStatus === "failed" ? "var(--destructive)" : nodeStatus === "succeeded" ? "var(--success)" : "var(--muted-foreground)";
      }} /></ReactFlow>
      {connectionHint && <div className="absolute bottom-3 left-3 z-10 flex max-w-md items-start gap-2 rounded-lg border border-amber-500/40 bg-background/95 px-3 py-2 text-caption shadow-sm" role="alert"><AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600" /><span>{connectionHint}</span></div>}
      <Sheet open={inspector && !!selected} onOpenChange={setInspector}><SheetContent className="overflow-y-auto"><SheetHeader><SheetTitle>{t(($) => $.configure)}</SheetTitle></SheetHeader>
        {selected && <div className="space-y-5 px-4 pb-6">
          <div className="space-y-2"><Label htmlFor="workflow-node-name">{t(($) => $.node_name)}</Label><Input id="workflow-node-name" value={selected.label} onChange={(event) => patchNode({ label: event.target.value })} disabled={readOnly} /></div>
          {isTaskNode(selected) && (() => {
            const mode = workflowTaskMode(selected);
            const actor = workflowTaskActor(selected);
            const patchInstructions = (instructions: string) => patchNode({ instructions, config: { ...(selected.config ?? {}), instructions } });
            return <>
              <fieldset className="space-y-2"><legend className="text-body font-medium">{t(($) => $.task_mode)}</legend><div className="grid grid-cols-3 gap-1" role="radiogroup" aria-label={t(($) => $.task_mode)}>
                {(["task", "review", "condition"] as const).map((nextMode) => <Button key={nextMode} type="button" size="sm" variant={mode === nextMode ? "default" : "outline"} aria-pressed={mode === nextMode} onClick={() => patchNode(workflowTaskModePatch(selected, nextMode))} disabled={readOnly}>{t(($) => $[`task_mode_${nextMode}`])}</Button>)}
              </div></fieldset>
              {mode !== "condition" && <>
                <div className="space-y-2"><Label>{mode === "review" ? t(($) => $.reviewer) : t(($) => $.task_executor)}</Label><WorkflowActorPicker agents={agents} members={members} value={actor} allowAgents={mode === "task"} onSelect={(nextActor) => patchNode(nextActor ? workflowTaskActorPatch(selected, nextActor) : { assignee: undefined, agentId: undefined })} disabled={readOnly} /><p className="text-caption text-muted-foreground">{mode === "review" ? t(($) => $.task_review_hint) : t(($) => $.task_actor_hint)}</p></div>
                <div className="space-y-2"><Label htmlFor="workflow-node-instructions">{t(($) => $.instructions)}</Label><Textarea id="workflow-node-instructions" rows={7} value={selected.instructions ?? ""} onChange={(event) => patchInstructions(event.target.value)} disabled={readOnly} /><p className="text-caption text-muted-foreground">{t(($) => $.instructions_hint)}</p></div>
                {mode === "task" && selected.type === "agent" && <fieldset className="space-y-3"><legend className="text-body font-medium">{t(($) => $.references)}</legend><p className="text-caption text-muted-foreground">{t(($) => $.references_hint)}</p>{graph.nodes.filter((node) => ancestors.includes(node.id) && node.type === "agent").map((node) => <label key={node.id} className="flex items-center gap-2 text-caption"><Checkbox checked={selected.inputRefs?.includes(node.id) ?? false} onCheckedChange={(checked) => patchNode({ inputRefs: checked ? [...(selected.inputRefs ?? []), node.id] : selected.inputRefs?.filter((id) => id !== node.id) })} /><span className="truncate">{node.label}</span></label>)}</fieldset>}
              </>}
              {mode === "condition" && <p className="text-caption text-muted-foreground">{t(($) => $.condition_hint)}</p>}
            </>;
          })()}
          {selected.type === "parallel" && <p className="text-caption text-muted-foreground">{t(($) => $.parallel_hint)}</p>}
        </div>}
      </SheetContent></Sheet>
    </div>
  );
}
