import { ArrowRight, Blocks, ShieldCheck, WandSparkles } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { HOOK_TEMPLATES } from "../../core/hook-ux";
import type { WorkflowHook } from "../../core/hook-types";

export function HookTemplatePicker({ onSelect, onBack }: { onSelect: (hook: WorkflowHook) => void; onBack?: () => void }) {
  return <div className="h-full overflow-y-auto bg-background p-4 sm:p-8">
    <div className="mx-auto max-w-5xl">
      <div className="flex items-start justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-wide text-primary">New hook</p><h1 className="mt-1 text-2xl font-semibold">Start with a proven recipe</h1><p className="mt-2 max-w-2xl text-sm text-muted-foreground">Hooks watch workflow events, narrow them to the right work, make a decision, and run safe follow-up actions. Every recipe starts in Dry run.</p></div>{onBack && <Button variant="outline" onClick={onBack}>Back to hooks</Button>}</div>
      <div className="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{HOOK_TEMPLATES.map((template, index) => <button key={template.id} type="button" className="group flex min-h-44 flex-col rounded-xl border bg-card p-5 text-left shadow-sm transition hover:border-primary hover:shadow-md" onClick={() => onSelect({ ...template.hook, actions: template.hook.actions.map((action) => ({ ...action, config: { ...action.config } })), bindings: template.hook.bindings.map((binding) => ({ ...binding })), conditions: template.hook.conditions.map((condition) => ({ ...condition })) })}>
        <span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">{template.id === "scratch" ? <Blocks className="size-4" /> : index % 2 === 0 ? <ShieldCheck className="size-4" /> : <WandSparkles className="size-4" />}</span>
        <strong className="mt-4 text-sm">{template.title}</strong><span className="mt-1 flex-1 text-sm text-muted-foreground">{template.description}</span><span className="mt-4 inline-flex items-center gap-1 text-xs font-semibold text-primary">Use this recipe <ArrowRight className="size-3" /></span>
      </button>)}</div>
      <div className="mt-6 rounded-lg border bg-muted/30 p-4 text-sm"><strong>How Hooks work</strong><p className="mt-1 text-muted-foreground">Set up When it runs, whether to Guide or enforce, and the Actions to take. Save it, then test it against a past event. Publishing stays locked until Dry run evidence exists.</p></div>
    </div>
  </div>;
}
