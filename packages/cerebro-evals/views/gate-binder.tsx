"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
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
      <NativeSelect className="w-full" aria-label="Eval" value={evalId} onChange={(e) => setEvalId(e.target.value)}>
        <NativeSelectOption value="">Select eval…</NativeSelectOption>
        {evals.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.title} · {item.version}</NativeSelectOption>)}
      </NativeSelect>
      <NativeSelect className="w-full" aria-label="Issue workflow" value={workflowId} onChange={(e) => setWorkflowId(e.target.value)}>
        <NativeSelectOption value="">Select Issue workflow…</NativeSelectOption>
        {workflows.map((item) => <NativeSelectOption key={item.id} value={item.id}>{item.name}</NativeSelectOption>)}
      </NativeSelect>
      <NativeSelect className="w-full capitalize" aria-label="Phase" value={phase} onChange={(e) => {
        const nextPhase = e.target.value as EvalBindingPhase;
        setPhase(nextPhase);
        if (nextPhase === "monitor") setBlocking(false);
      }}>
        {PHASES.map((p) => <NativeSelectOption key={p} value={p}>{p}</NativeSelectOption>)}
      </NativeSelect>
      <NativeSelect className="w-full" aria-label="Enforcement" value={blocking ? "block" : "warn"} disabled={phase === "monitor"} onChange={(e) => setBlocking(e.target.value === "block")}>
        <NativeSelectOption value="block">Block</NativeSelectOption>
        <NativeSelectOption value="warn">Warn only</NativeSelectOption>
      </NativeSelect>
      <Button disabled={!evalId || !workflowId || pending} onClick={submit}>{blocking ? "Add blocking gate" : "Add advisory gate"}</Button>
    </div>
  );
}
