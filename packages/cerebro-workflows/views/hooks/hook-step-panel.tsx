import type { HookCondition, WorkflowHook } from "../../core/hook-types";
import { HOOK_EVENT_OPTIONS } from "../../core/hook-types";
import type { HookStepKey } from "./hook-chain";

const inputClass = "h-9 w-full rounded-md border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

export function HookStepPanel({ step, hook, onChange }: { step: HookStepKey; hook: WorkflowHook; onChange: (hook: WorkflowHook) => void }) {
  const updateCondition = (index: number, patch: Partial<HookCondition>) => {
    onChange({ ...hook, conditions: hook.conditions.map((condition, current) => current === index ? { ...condition, ...patch } : condition) });
  };
  const updateAction = (index: number, type: string) => {
    const option = ACTION_OPTIONS.find((candidate) => candidate.value === type) ?? ACTION_OPTIONS[0];
    onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { type: option.value, label: option.label, config: {} } : action) });
  };
  const updateActionConfig = (index: number, key: string, value: string | number | boolean) => {
    onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { ...action, config: { ...action.config, [key]: value } } : action) });
  };

  return (
    <section aria-label={`${step} configuration`} className="p-6">
      <p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Configuration</p>
      <h2 className="mt-1 text-lg font-semibold">{panelTitle(step)}</h2>
      <p className="mb-5 mt-1 text-sm text-muted-foreground">{panelHelp(step)}</p>

      {step === "trigger" && <label className="grid gap-1.5 text-sm font-medium">Start this hook when
        <select className={inputClass} value={hook.event_type} onChange={(event) => onChange({ ...hook, event_type: event.target.value as WorkflowHook["event_type"] })}>
          <optgroup label="Common hooks">{HOOK_EVENT_OPTIONS.filter((option) => option.advanced !== true).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</optgroup>
          <optgroup label="Advanced hooks">{HOOK_EVENT_OPTIONS.filter((option) => option.advanced === true).map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</optgroup>
        </select>
      </label>}

      {step === "scope" && <div className="grid gap-4">
        <label className="grid gap-1.5 text-sm font-medium">Scope
          <select className={inputClass} value={hook.binding.kind} onChange={(event) => onChange({ ...hook, binding: { ...hook.binding, kind: event.target.value as WorkflowHook["binding"]["kind"] } })}>
            <option value="model">Agent or model</option><option value="issue">Issue</option><option value="session">Chat or session</option><option value="project">Project</option><option value="workflow">Workflow</option><option value="workspace">Workspace</option>
          </select>
        </label>
        <label className="grid gap-1.5 text-sm font-medium">Match value<input className={inputClass} value={hook.binding.value} onChange={(event) => onChange({ ...hook, binding: { ...hook.binding, value: event.target.value } })} /></label>
      </div>}

      {step === "filter" && <div className="grid gap-2">
        {hook.conditions.map((condition, index) => <div key={`${condition.field}-${index}`}>
          <div className="grid grid-cols-[1fr_9rem_1fr] gap-2">
            <input aria-label={`Filter field ${index + 1}`} className={inputClass} value={condition.field} onChange={(event) => updateCondition(index, { field: event.target.value })} />
            <select aria-label={`Filter operator ${index + 1}`} className={inputClass} value={condition.operator} onChange={(event) => updateCondition(index, { operator: event.target.value })}>
              <option value="eq">is</option><option value="not_in">is not one of</option><option value="exists">exists</option><option value="not_exists">does not exist</option><option value="starts_with">starts with</option><option value="gte">is at least</option><option value="lt">is below</option>
            </select>
            <input aria-label={`Filter value ${index + 1}`} className={inputClass} value={condition.value} disabled={condition.operator === "exists" || condition.operator === "not_exists"} onChange={(event) => updateCondition(index, { value: event.target.value })} />
          </div>
          {index < hook.conditions.length - 1 && <div className="py-1 text-center text-[10px] font-bold tracking-wider text-[#5b5bd6]">{condition.conjunction ?? "AND"}</div>}
        </div>)}
        <button type="button" className="mt-2 w-fit rounded-md border px-3 py-2 text-sm" onClick={() => onChange({ ...hook, conditions: [...hook.conditions, { field: "", operator: "eq", value: "", conjunction: "AND" }] })}>+ Add condition</button>
      </div>}

      {step === "decision" && <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">When the filter matches</legend>{(["allow", "block", "modify", "require"] as const).map((decision) => <label key={decision} className={`flex items-start gap-3 rounded-lg border p-3 ${hook.decision === decision ? "border-[#5b5bd6] bg-[#eeeefc]" : ""}`}><input type="radio" name="decision" checked={hook.decision === decision} onChange={() => onChange({ ...hook, decision })} /><span><strong className="capitalize">{decision}</strong><span className="block text-xs text-muted-foreground">{decisionDescription(decision)}</span></span></label>)}</fieldset>}

      {step === "action" && <div className="grid gap-4">
        <label className="grid gap-1.5 text-sm font-medium">Required outcome<textarea className="min-h-20 rounded-md border border-input bg-background p-3 text-sm" value={hook.requirement} onChange={(event) => onChange({ ...hook, requirement: event.target.value })} /></label>
        {hook.actions.map((action, index) => <div key={`${action.type}-${index}`} className="grid gap-3 rounded-lg border p-4">
          <label className="grid gap-1.5 text-sm font-medium">Action {index + 1}
            <select aria-label={`Action ${index + 1} type`} className={inputClass} value={action.type} onChange={(event) => updateAction(index, event.target.value)}>
              {ACTION_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
          </label>
          {action.type === "session.handoff" && <div className="grid gap-3">
            <label className="grid gap-1 text-sm">Target agent<input aria-label="Handoff target" className={inputClass} value={String(action.config.target ?? "")} onChange={(event) => updateActionConfig(index, "target", event.target.value)} /></label>
            <label className="grid gap-1 text-sm">Plan reference<input aria-label="Handoff plan reference" className={inputClass} value={String(action.config.plan_ref ?? "")} onChange={(event) => updateActionConfig(index, "plan_ref", event.target.value)} /></label>
            <label className="flex items-center gap-2 text-sm"><input aria-label="Start new session now" type="checkbox" checked={action.config.start_new !== false} onChange={(event) => updateActionConfig(index, "start_new", event.target.checked)} />Start new session now</label>
            <label className="grid gap-1 text-sm">Summary<textarea aria-label="Handoff summary" className="min-h-16 rounded-md border border-input bg-background p-3 text-sm" value={String(action.config.summary ?? "")} onChange={(event) => updateActionConfig(index, "summary", event.target.value)} /></label>
            <label className="grid gap-1 text-sm">Done<textarea aria-label="Handoff done" className="min-h-16 rounded-md border border-input bg-background p-3 text-sm" value={String(action.config.done ?? "")} onChange={(event) => updateActionConfig(index, "done", event.target.value)} /></label>
            <label className="grid gap-1 text-sm">Remaining<textarea aria-label="Handoff remaining" className="min-h-16 rounded-md border border-input bg-background p-3 text-sm" value={String(action.config.remaining ?? "")} onChange={(event) => updateActionConfig(index, "remaining", event.target.value)} /></label>
            <label className="grid gap-1 text-sm">Maximum Handoff depth<input aria-label="Maximum Handoff depth" type="number" min={1} max={4} className={inputClass} value={Number(action.config.max_depth ?? 2)} onChange={(event) => updateActionConfig(index, "max_depth", Number(event.target.value))} /></label>
          </div>}
        </div>)}
        <button type="button" className="w-fit rounded-md border px-3 py-2 text-sm" onClick={() => onChange({ ...hook, actions: [...hook.actions, { type: "audit.record", label: "Record audit event", config: {} }] })}>+ Add action</button>
      </div>}

      {step === "failure" && <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">If the hook cannot be evaluated</legend>{(["open", "closed", "warn"] as const).map((mode) => <label key={mode} className="flex items-center gap-3 rounded-lg border p-3"><input type="radio" name="fail-mode" checked={hook.fail_mode === mode} onChange={() => onChange({ ...hook, fail_mode: mode })} /><span className="capitalize">Fail {mode}</span></label>)}</fieldset>}
    </section>
  );
}

function panelTitle(step: HookStepKey) { return ({ trigger: "Choose the trigger", scope: "Choose what this applies to", filter: "Continue only if…", decision: "Choose the decision", action: "Choose the required action", failure: "Choose failure behavior" })[step]; }
function panelHelp(step: HookStepKey) { return ({ trigger: "Technical event names are grouped under Advanced hooks.", scope: "The same chain works for agents, issues, workflows, and chats.", filter: "Every condition is an explainable fact. No raw JSON is required.", decision: "A block always wins when multiple policies match.", action: "The actor receives this concrete remediation.", failure: "Safety hooks can fail closed; ordinary automation should fail open." })[step]; }
function decisionDescription(decision: WorkflowHook["decision"]) { return ({ allow: "Continue without changing the proposed action.", block: "Stop and return a concrete remediation.", modify: "Change only fields declared mutable by the event.", require: "Require one or more outcomes before continuing." })[decision]; }

const ACTION_OPTIONS = [
  { value: "member.notify", label: "Notify member" },
  { value: "agent.dispatch", label: "Start agent" },
  { value: "squad.dispatch", label: "Start squad" },
  { value: "wakeup.create", label: "Create wakeup" },
  { value: "wakeup.cancel", label: "Cancel wakeup" },
  { value: "session.handoff", label: "Start Handoff" },
  { value: "task.retry", label: "Repeat current step" },
  { value: "task.cancel", label: "Cancel task" },
  { value: "artifact.create_or_update", label: "Create or update artifact" },
  { value: "workflow.activate", label: "Start workflow" },
  { value: "workflow.pause", label: "Pause workflow" },
  { value: "workflow.resume", label: "Resume workflow" },
  { value: "workflow.stop", label: "Stop workflow" },
  { value: "approval.require", label: "Require approval" },
  { value: "audit.record", label: "Record audit event" },
  { value: "metric.increment", label: "Increment metric" },
] as const;
