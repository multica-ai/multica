"use client";

import type { AppWorkflowDefinition, WorkflowStepType } from "../core";

const triggerLabels = {
  schedule: "On a schedule",
  webhook: "When a webhook arrives",
  data_event: "When registry data changes",
  manual: "When started manually",
  chat: "When requested in chat",
} as const;
const stepLabels: Record<WorkflowStepType, string> = {
  "registry.read": "Read registry data",
  "registry.write": "Write registry data",
  filter: "Continue only if…",
  "view.show_and_wait": "Show a view and wait",
};

export function WorkflowBuilder({ value, onChange, onTestStep }: { value: AppWorkflowDefinition; onChange: (value: AppWorkflowDefinition) => void; onTestStep?: (stepId: string, sample: unknown) => void }) {
  const addStep = () => onChange({ ...value, steps: [...value.steps, { id: `step-${value.steps.length + 1}`, type: "registry.read", config: {} }] });
  return <div className="mx-auto flex w-full max-w-2xl flex-col items-stretch gap-0" aria-label="Workflow steps">
    <NodeCard eyebrow="Trigger" title={triggerLabels[value.trigger.type]} onTest={() => onTestStep?.(value.trigger.id, {})} />
    {value.steps.map((step) => <div key={step.id}>
      <Connector />
      <NodeCard eyebrow="Step" title={stepLabels[step.type]} onTest={() => onTestStep?.(step.id, {})} />
    </div>)}
    <Connector />
    <button type="button" onClick={addStep} className="self-center rounded-full border border-dashed px-4 py-2 text-sm font-medium hover:bg-muted">Add step</button>
  </div>;
}

function Connector() { return <div className="mx-auto h-8 w-px border-l border-dashed border-border" aria-hidden="true" />; }
function NodeCard({ eyebrow, title, onTest }: { eyebrow: string; title: string; onTest: () => void }) {
  return <section className="rounded-xl border bg-card p-4 shadow-sm">
    <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{eyebrow}</div>
    <div className="mt-1 flex items-center justify-between gap-4"><h3 className="font-semibold">{title}</h3><button type="button" onClick={onTest} className="rounded-md border px-3 py-1.5 text-sm">Test</button></div>
  </section>;
}
