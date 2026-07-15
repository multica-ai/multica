import { Check, ChevronRight, CircleAlert, Filter, Play, Target, ShieldAlert, Zap } from "lucide-react";

export type HookStepKey = "trigger" | "scope" | "filter" | "decision" | "action" | "failure";

const STEPS: ReadonlyArray<{ key: HookStepKey; label: string; summary: string; icon: typeof Zap }> = [
  { key: "trigger", label: "Trigger", summary: "Before task completes", icon: Zap },
  { key: "scope", label: "Applies to", summary: "Agent, issue, or session", icon: Target },
  { key: "filter", label: "Filter", summary: "Continue only if…", icon: Filter },
  { key: "decision", label: "Decision", summary: "Block the action", icon: ShieldAlert },
  { key: "action", label: "Action", summary: "Require a continuation", icon: Play },
  { key: "failure", label: "On hook failure", summary: "Fail closed", icon: CircleAlert },
];

export function HookChain({ selected, onSelect }: { selected: HookStepKey; onSelect: (step: HookStepKey) => void }) {
  return (
    <ol aria-label="Hook chain" className="flex flex-col px-6 py-5">
      {STEPS.map((step, index) => {
        const Icon = step.icon;
        return (
          <li key={step.key} className="contents">
            <button
              type="button"
              aria-label={`Configure ${step.label}`}
              onClick={() => onSelect(step.key)}
              className={`group flex min-h-20 w-full items-start gap-3 rounded-lg border bg-background p-3 text-left shadow-sm transition ${selected === step.key ? "border-[#5b5bd6] ring-2 ring-[#eeeefc]" : "border-border hover:border-muted-foreground/40"}`}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-[#eeeefc] text-[#5b5bd6]"><Icon className="size-4" /></span>
              <span className="min-w-0 flex-1">
                <span className="block text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">{index + 1} · {step.label}</span>
                <span className="mt-0.5 block text-sm font-semibold">{step.summary}</span>
              </span>
              <span className="flex size-4 shrink-0 items-center justify-center rounded-full bg-emerald-600 text-white"><Check className="size-2.5" /></span>
            </button>
            {index < STEPS.length - 1 && (
              <div aria-hidden="true" className="flex h-9 items-center justify-center">
                <span className="h-full w-px bg-border" />
                <span className="absolute flex size-5 items-center justify-center rounded-full border bg-background text-muted-foreground">+</span>
                <ChevronRight className="sr-only" />
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
