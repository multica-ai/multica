"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import type { EvalBindingPhase } from "../types";

export interface GateBinderEval {
  id: string;
  title: string;
  version: string;
}

export interface GateBinderWorkflow {
  id: string;
  name: string;
}

export interface GateBinderProps {
  evals: GateBinderEval[];
  workflows: GateBinderWorkflow[];
  pending?: boolean;
  onBind: (input: { workflowId: string; evalId: string; phase: EvalBindingPhase; blocking: boolean }) => void;
}

const PHASES: EvalBindingPhase[] = ["plan", "delivery", "monitor"];

// GateBinder is the "Workflow gates" create control: pick an eval, an Issue
// workflow, the phase it gates (plan / delivery / monitor) and whether it Blocks
// or only Warns. A blocking binding holds the workflow at that phase until the
// latest issue-specific run passes; Warn-only never blocks and only notifies.
export function GateBinder({ evals, workflows, pending = false, onBind }: GateBinderProps) {
  const [evalId, setEvalId] = useState("");
  const [workflowId, setWorkflowId] = useState("");
  const [phase, setPhase] = useState<EvalBindingPhase>("delivery");
  const [blocking, setBlocking] = useState(true);

  const submit = () => {
    if (!evalId || !workflowId) return;
    onBind({ workflowId, evalId, phase, blocking: phase === "monitor" ? false : blocking });
    setEvalId("");
    setWorkflowId("");
    setPhase("delivery");
    setBlocking(true);
  };

  return (
    <div className="grid gap-2 md:grid-cols-[1fr_1fr_auto_auto_auto]">
      <select aria-label="Eval" className="h-9 rounded-md border bg-background px-3 text-sm" value={evalId} onChange={(e) => setEvalId(e.target.value)}>
        <option value="">Select eval…</option>
        {evals.map((item) => <option key={item.id} value={item.id}>{item.title} · {item.version}</option>)}
      </select>
      <select aria-label="Issue workflow" className="h-9 rounded-md border bg-background px-3 text-sm" value={workflowId} onChange={(e) => setWorkflowId(e.target.value)}>
        <option value="">Select Issue workflow…</option>
        {workflows.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select>
      <select aria-label="Phase" className="h-9 rounded-md border bg-background px-3 text-sm capitalize" value={phase} onChange={(e) => {
        const nextPhase = e.target.value as EvalBindingPhase;
        setPhase(nextPhase);
        if (nextPhase === "monitor") setBlocking(false);
      }}>
        {PHASES.map((p) => <option key={p} value={p}>{p}</option>)}
      </select>
      <select aria-label="Enforcement" className="h-9 rounded-md border bg-background px-3 text-sm" value={blocking ? "block" : "warn"} disabled={phase === "monitor"} onChange={(e) => setBlocking(e.target.value === "block")}>
        <option value="block">Block</option>
        <option value="warn">Warn only</option>
      </select>
      <Button disabled={!evalId || !workflowId || pending} onClick={submit}>{blocking ? "Add blocking gate" : "Add advisory gate"}</Button>
    </div>
  );
}
