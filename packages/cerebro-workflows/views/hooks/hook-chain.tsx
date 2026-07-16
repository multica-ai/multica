import { Check, Circle, CircleAlert, Filter, Play, Target, ShieldAlert, Zap } from "lucide-react";
import type { WorkflowHook } from "../../core/hook-types";
import { validateHookStep } from "../../core/hook-validation";
import { stepSummary } from "../../core/hook-ux";

export type HookStepKey = "trigger" | "scope" | "filter" | "decision" | "action" | "failure";

const STEPS: ReadonlyArray<{ key: HookStepKey; label: string; icon: typeof Zap }> = [
  { key: "trigger", label: "Trigger", icon: Zap },
  { key: "scope", label: "Applies to", icon: Target },
  { key: "filter", label: "Filter", icon: Filter },
  { key: "decision", label: "Decision", icon: ShieldAlert },
  { key: "action", label: "Action", icon: Play },
  { key: "failure", label: "On hook failure", icon: CircleAlert },
];

export function HookChain({ selected, hook, onSelect }: { selected: HookStepKey; hook: WorkflowHook; onSelect: (step: HookStepKey) => void }) {
  return (
    <ol aria-label="Hook chain" className="flex flex-col px-6 py-5">
      {STEPS.map((step, index) => {
        const Icon = step.icon;
        const status = validateHookStep(hook, step.key);
        return (
          <li key={step.key} className="contents">
            <button
              type="button"
              aria-label={`Configure ${step.label}`}
              data-state={status.valid ? "complete" : "incomplete"}
              onClick={() => onSelect(step.key)}
              className={`group flex min-h-20 w-full items-start gap-3 rounded-lg border bg-background p-3 text-left shadow-sm transition ${selected === step.key ? "border-primary ring-2 ring-primary/15" : "border-border hover:border-muted-foreground/40"}`}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary"><Icon className="size-4" /></span>
              <span className="min-w-0 flex-1">
                <span className="block text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">{index + 1} · {step.label}</span>
                <span className="mt-0.5 block text-sm font-semibold">{stepSummary(hook, step.key)}</span>
              </span>
              <span title={status.message} className={`flex size-4 shrink-0 items-center justify-center rounded-full ${status.valid ? "bg-success text-success-foreground" : "border border-muted-foreground/40 text-muted-foreground"}`}>{status.valid ? <Check className="size-2.5" /> : <Circle className="size-2" />}</span>
            </button>
            {index < STEPS.length - 1 && <div aria-hidden="true" className="mx-auto h-9 w-px bg-border" />}
          </li>
        );
      })}
    </ol>
  );
}
