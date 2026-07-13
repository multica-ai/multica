"use client";

import { useEffect, useState } from "react";
import { ChevronRight, Play, Plus } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import type { AppWorkflowDefinition, WorkflowNode, WorkflowStepType } from "../core";

const triggerLabels = { schedule: "On a schedule", webhook: "When a webhook arrives", data_event: "When registry data changes", manual: "When started manually", chat: "When requested in chat" } as const;
const stepLabels: Record<WorkflowStepType, string> = { "registry.read": "Read registry data", "registry.write": "Write registry data", filter: "Continue only if…", "view.show_and_wait": "Show a view and wait" };
const stepHelp: Record<WorkflowStepType, string> = { "registry.read": "Load data for the next step", "registry.write": "Save the previous step's result", filter: "Continue when the rule matches", "view.show_and_wait": "Ask a person before continuing" };

export function WorkflowBuilder({ value, onChange, onTestStep }: { value: AppWorkflowDefinition; onChange: (value: AppWorkflowDefinition) => void; onTestStep?: (stepId: string, sample: unknown) => void }) {
  const [selectedId, setSelectedId] = useState(value.steps[0]?.id ?? "");
  const [sampleText, setSampleText] = useState('{"example":true}');
  const [resourceId, setResourceId] = useState("");
  const [sampleError, setSampleError] = useState("");
  const selected = value.steps.find((step) => step.id === selectedId);
  useEffect(() => { setResourceId(typeof selected?.config.resource_id === "string" ? selected.config.resource_id : ""); }, [selectedId, selected?.config.resource_id]);

  const updateStep = (patch: Partial<WorkflowNode<WorkflowStepType>>) => {
    if (!selected) return;
    onChange({ ...value, steps: value.steps.map((step) => step.id === selected.id ? { ...step, ...patch } : step) });
  };
  const addStep = () => {
    const id = `step-${value.steps.length + 1}`;
    onChange({ ...value, steps: [...value.steps, { id, type: "registry.read", config: {} }] });
    setSelectedId(id);
  };

  return <div className="space-y-4" aria-label="Workflow steps">
    <div className="rounded-lg bg-muted/40 p-3">
      <NodeCard eyebrow="Trigger" index="01" title={triggerLabels[value.trigger.type]} description="The event that starts this workflow" />
      {value.steps.map((step, index) => <div key={step.id}>
        <Connector />
        <NodeCard eyebrow={`Step ${index + 1}`} index={String(index + 2).padStart(2, "0")} title={stepLabels[step.type]} description={stepHelp[step.type]} selected={selectedId === step.id} onConfigure={() => setSelectedId(step.id)} />
      </div>)}
      <Connector />
      <Button type="button" variant="outline" size="sm" onClick={addStep} className="mx-auto flex border-dashed"><Plus className="size-3.5" />Add step</Button>
    </div>
    {selected && <div className="space-y-4 rounded-lg border bg-background p-4">
      <div><h3 className="text-sm font-semibold">Configure {stepLabels[selected.type]}</h3><p className="text-xs text-muted-foreground">Choose the action, map its field, then test it with sample data.</p></div>
      <Label className="grid gap-2"><span>Action</span><NativeSelect value={selected.type} onChange={(event) => updateStep({ type: event.target.value as WorkflowStepType, config: {} })}>{Object.entries(stepLabels).map(([type, label]) => <option key={type} value={type}>{label}</option>)}</NativeSelect></Label>
      {(selected.type === "registry.read" || selected.type === "registry.write") && <Label className="grid gap-2"><span>Resource ID</span><Input value={resourceId} onChange={(event) => { setResourceId(event.target.value); updateStep({ config: { ...selected.config, resource_id: event.target.value } }); }} placeholder="products" /></Label>}
      {selected.type === "filter" && <div className="grid gap-3 sm:grid-cols-3">
        <Label className="grid gap-2"><span>Source field</span><Input value={typeof selected.config.field === "string" ? selected.config.field : ""} onChange={(event) => updateStep({ config: { ...selected.config, field: event.target.value } })} placeholder="read.count" /></Label>
        <Label className="grid gap-2"><span>Condition</span><NativeSelect value={typeof selected.config.operator === "string" ? selected.config.operator : "eq"} onChange={(event) => updateStep({ config: { ...selected.config, operator: event.target.value } })}>{["eq", "neq", "gt", "gte", "lt", "lte", "contains"].map((operator) => <option key={operator} value={operator}>{operator}</option>)}</NativeSelect></Label>
        <Label className="grid gap-2"><span>Compare value</span><Input value={typeof selected.config.value === "string" || typeof selected.config.value === "number" ? selected.config.value : ""} onChange={(event) => { const numeric = Number(event.target.value); updateStep({ config: { ...selected.config, value: event.target.value !== "" && Number.isFinite(numeric) ? numeric : event.target.value } }); }} placeholder="0" /></Label>
      </div>}
      {selected.type === "view.show_and_wait" && <Label className="grid gap-2"><span>View ID</span><Input value={typeof selected.config.view_id === "string" ? selected.config.view_id : ""} onChange={(event) => updateStep({ config: { ...selected.config, view_id: event.target.value } })} placeholder="approve" /></Label>}
      <Label className="grid gap-2"><span>Sample data</span><Textarea value={sampleText} onChange={(event) => setSampleText(event.target.value)} rows={4} className="font-mono text-xs" /></Label>
      {sampleError && <p role="alert" className="text-xs text-destructive">{sampleError}</p>}
      <Button type="button" size="sm" variant="outline" onClick={() => { try { const sample = JSON.parse(sampleText) as unknown; setSampleError(""); onTestStep?.(selected.id, sample); } catch { setSampleError("Sample data must be valid JSON"); } }}><Play className="size-3.5" />Test step</Button>
    </div>}
  </div>;
}

function Connector() { return <div className="mx-auto flex h-7 w-6 items-center justify-center text-muted-foreground" aria-hidden="true"><ChevronRight className="size-4 rotate-90" /></div>; }
function NodeCard({ eyebrow, index, title, description, selected, onConfigure }: { eyebrow: string; index: string; title: string; description: string; selected?: boolean; onConfigure?: () => void }) {
  const body = <><span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted font-mono text-[11px] text-muted-foreground">{index}</span><span className="min-w-0 flex-1"><span className="block text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">{eyebrow}</span><span className="block text-sm font-semibold">{title}</span><span className="block truncate text-xs text-muted-foreground">{description}</span></span></>;
  if (!onConfigure) return <div className="flex items-center gap-3 rounded-lg border bg-card p-3">{body}</div>;
  return <button type="button" aria-label={`Configure ${title}`} onClick={onConfigure} className={`flex w-full items-center gap-3 rounded-lg border p-3 text-left transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selected ? "border-primary/40 bg-primary/5" : "bg-card"}`}>{body}</button>;
}
