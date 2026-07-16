import { Plus, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import type { HookBinding, HookCondition, HookEventType, WorkflowHook } from "../../core/hook-types";
import { HOOK_EVENT_OPTIONS } from "../../core/hook-types";
import { fieldsForEvents, validateHookStep } from "../../core/hook-validation";
import type { HookStepKey } from "./hook-chain";
import { HookTargetPicker, type HookDirectory } from "./hook-target-picker";

const scopeOptions: Array<{ value: HookBinding["kind"]; label: string }> = [
  { value: "agent", label: "Agent" }, { value: "model", label: "Model" }, { value: "issue", label: "Issue" },
  { value: "session", label: "Chat or session" }, { value: "project", label: "Project" },
  { value: "workflow", label: "Workflow" }, { value: "workspace", label: "Workspace" },
];
const operatorOptions = [
  { value: "eq", label: "is" }, { value: "not_in", label: "is not one of" },
  { value: "exists", label: "exists" }, { value: "not_exists", label: "does not exist" },
  { value: "starts_with", label: "starts with" }, { value: "gte", label: "is at least" }, { value: "lt", label: "is below" },
];

function ChoiceSelect({ label, value, options, onChange, placeholder = "Select an option" }: { label: string; value: string; options: ReadonlyArray<{ value: string; label: string }>; onChange: (value: string) => void; placeholder?: string }) {
  return <Select value={value || null} onValueChange={(next) => next && onChange(next)}>
    <SelectTrigger aria-label={label} className="w-full"><SelectValue>{options.find((option) => option.value === value)?.label ?? placeholder}</SelectValue></SelectTrigger>
    <SelectContent>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
  </Select>;
}

export function HookStepPanel({ step, hook, onChange, directory = {} }: { step: HookStepKey; hook: WorkflowHook; onChange: (hook: WorkflowHook) => void; directory?: HookDirectory }) {
  const validation = validateHookStep(hook, step);
  const fields = fieldsForEvents(hook.events);
  const updateCondition = (index: number, patch: Partial<HookCondition>) => onChange({ ...hook, conditions: hook.conditions.map((condition, current) => current === index ? { ...condition, ...patch } : condition) });
  const updateAction = (index: number, type: string) => {
    const option = ACTION_OPTIONS.find((candidate) => candidate.value === type) ?? ACTION_OPTIONS[0];
    onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { type: option.value, label: option.label, config: {} } : action) });
  };
  const updateActionConfig = (index: number, key: string, value: string | number | boolean) => onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { ...action, config: { ...action.config, [key]: value } } : action) });

  return <section aria-label={`${step} configuration`} className="p-4 sm:p-6">
    <p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Configuration</p>
    <h2 className="mt-1 text-lg font-semibold">{panelTitle(step)}</h2>
    <p className="mt-1 text-sm text-muted-foreground">{panelHelp(step)}</p>
    {!validation.valid && <p role="alert" className="mb-5 mt-2 text-sm text-destructive">{validation.message}</p>}
    {validation.valid && <div className="mb-5" />}

    {step === "trigger" && <div className="grid gap-3">
      {hook.events.map((eventType, index) => <div key={`${eventType}-${index}`} className="flex items-end gap-2">
        <label className="grid flex-1 gap-1.5 text-sm font-medium">Trigger {index + 1}<ChoiceSelect label={`Trigger ${index + 1}`} value={eventType} options={HOOK_EVENT_OPTIONS} onChange={(value) => onChange({ ...hook, events: hook.events.map((current, currentIndex) => currentIndex === index ? value as HookEventType : current) })} /></label>
        <Button type="button" variant="outline" size="icon" aria-label={`Remove trigger ${index + 1}`} onClick={() => onChange({ ...hook, events: hook.events.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
      </div>)}
      <Button type="button" variant="outline" className="w-fit" aria-label="Add trigger" onClick={() => onChange({ ...hook, events: [...hook.events, "before.task.complete"] })}><Plus className="size-4" />Add trigger</Button>
    </div>}

    {step === "scope" && <div className="grid gap-3">
      {hook.bindings.map((binding, index) => <div key={`${binding.kind}-${index}`} className="grid gap-2 rounded-lg border p-3 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-end">
        <label className="grid gap-1.5 text-sm font-medium">Scope<ChoiceSelect label={`Scope ${index + 1}`} value={binding.kind} options={scopeOptions} onChange={(value) => onChange({ ...hook, bindings: hook.bindings.map((current, currentIndex) => currentIndex === index ? { kind: value as HookBinding["kind"], value: "" } : current) })} /></label>
        {binding.kind === "workspace" ? <p className="flex h-9 items-center text-sm text-muted-foreground">This workspace</p> : <label className="grid gap-1.5 text-sm font-medium">Match value<HookTargetPicker label={scopeOptions.find((option) => option.value === binding.kind)?.label ?? "Target"} value={binding.value} options={directory[binding.kind] ?? []} onChange={(value) => onChange({ ...hook, bindings: hook.bindings.map((current, currentIndex) => currentIndex === index ? { ...current, value } : current) })} /></label>}
        <Button type="button" variant="outline" size="icon" aria-label={`Remove scope ${index + 1}`} onClick={() => onChange({ ...hook, bindings: hook.bindings.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
      </div>)}
      <Button type="button" variant="outline" className="w-fit" aria-label="Add scope" onClick={() => onChange({ ...hook, bindings: [...hook.bindings, { kind: "workspace", value: "" }] })}><Plus className="size-4" />Add scope</Button>
    </div>}

    {step === "filter" && <div className="grid gap-2">
      {hook.conditions.map((condition, index) => <div key={index} className="grid gap-2"><div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto] sm:items-end">
        <label className="grid gap-1 text-sm">Field<ChoiceSelect label={`Filter field ${index + 1}`} value={condition.field} placeholder="Select field" options={fields.map((field) => ({ value: field, label: field }))} onChange={(value) => updateCondition(index, { field: value })} /></label>
        <label className="grid gap-1 text-sm">Operator<ChoiceSelect label={`Filter operator ${index + 1}`} value={condition.operator} options={operatorOptions} onChange={(value) => updateCondition(index, { operator: value })} /></label>
        <label className="grid gap-1 text-sm">Value<Input aria-label={`Filter value ${index + 1}`} value={condition.value} disabled={["exists", "not_exists"].includes(condition.operator)} onChange={(event) => updateCondition(index, { value: event.target.value })} /></label>
        <Button type="button" variant="outline" size="icon" aria-label={`Remove condition ${index + 1}`} onClick={() => onChange({ ...hook, conditions: hook.conditions.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
      </div>{index < hook.conditions.length - 1 && <span className="justify-self-center rounded-full border bg-muted px-2 py-0.5 text-[10px] font-bold text-muted-foreground">AND</span>}</div>)}
      <Button type="button" variant="outline" className="mt-2 w-fit" aria-label="Add condition" disabled={hook.events.length === 0} onClick={() => onChange({ ...hook, conditions: [...hook.conditions, { field: fields[0] ?? "", operator: "eq", value: "", conjunction: "AND" }] })}><Plus className="size-4" />Add condition</Button>
    </div>}

    {step === "decision" && <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">When the filter matches</legend><RadioGroup value={hook.decision} onValueChange={(value) => onChange({ ...hook, decision: value as WorkflowHook["decision"] })}>{(["allow", "block", "modify", "require"] as const).map((decision) => <label key={decision} className={`flex items-start gap-3 rounded-lg border p-3 ${hook.decision === decision ? "border-primary bg-primary/10" : ""}`}><RadioGroupItem value={decision} aria-label={decision} /><span><strong className="capitalize">{decision}</strong><span className="block text-xs text-muted-foreground">{decisionDescription(decision)}</span></span></label>)}</RadioGroup></fieldset>}

    {step === "action" && <div className="grid gap-4">
      <label className="grid gap-1.5 text-sm font-medium">Required outcome<Textarea className="min-h-20" value={hook.requirement} onChange={(event) => onChange({ ...hook, requirement: event.target.value })} /></label>
      {hook.actions.map((action, index) => <div key={index} className="grid gap-3 rounded-lg border p-4">
        <div className="flex items-end gap-2"><label className="grid flex-1 gap-1.5 text-sm font-medium">Action {index + 1}<ChoiceSelect label={`Action ${index + 1} type`} value={action.type} options={ACTION_OPTIONS} onChange={(value) => updateAction(index, value)} /></label><Button type="button" variant="outline" size="icon" aria-label={`Remove action ${index + 1}`} onClick={() => onChange({ ...hook, actions: hook.actions.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button></div>
        <ActionConfiguration action={action} directory={directory} update={(key, value) => updateActionConfig(index, key, value)} />
      </div>)}
      <Button type="button" variant="outline" className="w-fit" aria-label="Add action" onClick={() => onChange({ ...hook, actions: [...hook.actions, { type: "audit.record", label: "Record audit event", config: {} }] })}><Plus className="size-4" />Add action</Button>
    </div>}

    {step === "failure" && <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">If the hook cannot be evaluated</legend><RadioGroup value={hook.fail_mode} onValueChange={(value) => onChange({ ...hook, fail_mode: value as WorkflowHook["fail_mode"] })}>{(["open", "closed", "warn"] as const).map((mode) => <label key={mode} className="flex items-center gap-3 rounded-lg border p-3"><RadioGroupItem value={mode} aria-label={`Fail ${mode}`} /><span className="capitalize">Fail {mode}</span></label>)}</RadioGroup></fieldset>}
  </section>;
}

function ActionConfiguration({ action, directory, update }: { action: WorkflowHook["actions"][number]; directory: HookDirectory; update: (key: string, value: string | number | boolean) => void }) {
  if (action.type === "agent.dispatch") return <label className="grid gap-1 text-sm">Agent<HookTargetPicker label="Agent" value={String(action.config.agent_id ?? "")} options={directory.agent ?? []} onChange={(value) => update("agent_id", value)} /></label>;
  if (action.type === "squad.dispatch") return <div className="grid gap-3"><label className="grid gap-1 text-sm">Squad<HookTargetPicker label="Squad" value={String(action.config.squad_id ?? "")} options={directory.squad ?? []} onChange={(value) => update("squad_id", value)} /></label><label className="grid gap-1 text-sm">Lead agent<HookTargetPicker label="Lead agent" value={String(action.config.agent_id ?? "")} options={directory.agent ?? []} onChange={(value) => update("agent_id", value)} /></label></div>;
  if (["workflow.activate", "workflow.pause", "workflow.resume", "workflow.stop"].includes(action.type)) return <label className="grid gap-1 text-sm">Workflow<HookTargetPicker label="Workflow" value={String(action.config.workflow_id ?? "")} options={directory.workflow ?? []} onChange={(value) => update("workflow_id", value)} /></label>;
  if (action.type === "skill.run") return <div className="grid gap-3"><label className="grid gap-1 text-sm">Skill<HookTargetPicker label="Skill" value={String(action.config.skill_name ?? "")} options={directory.skill ?? []} onChange={(value) => update("skill_name", value)} /></label><label className="grid gap-1 text-sm">Agent (optional)<HookTargetPicker label="Agent" value={String(action.config.agent_id ?? "")} options={directory.agent ?? []} onChange={(value) => update("agent_id", value)} /></label></div>;
  if (action.type === "judge.gate") return <div className="grid gap-3"><label className="grid gap-1 text-sm">Judge agent<HookTargetPicker label="Judge agent" value={String(action.config.agent_id ?? "")} options={directory.agent ?? []} onChange={(value) => update("agent_id", value)} /></label><label className="grid gap-1 text-sm">Rubric<Textarea aria-label="Judge rubric" value={String(action.config.rubric ?? "")} onChange={(event) => update("rubric", event.target.value)} /></label></div>;
  if (action.type !== "session.handoff") return null;
  return <div className="grid gap-3">
    <label className="grid gap-1 text-sm">Target agent<HookTargetPicker label="Handoff target" value={String(action.config.target ?? "")} options={directory.agent ?? []} onChange={(value) => update("target", value)} /></label>
    <label className="grid gap-1 text-sm">Plan reference<Input aria-label="Handoff plan reference" value={String(action.config.plan_ref ?? "")} onChange={(event) => update("plan_ref", event.target.value)} /></label>
    <label className="flex items-center gap-2 text-sm"><Checkbox aria-label="Start new session now" checked={action.config.start_new !== false} onCheckedChange={(checked) => update("start_new", checked === true)} />Start new session now</label>
    <label className="grid gap-1 text-sm">Summary<Textarea aria-label="Handoff summary" value={String(action.config.summary ?? "")} onChange={(event) => update("summary", event.target.value)} /></label>
    <label className="grid gap-1 text-sm">Done<Textarea aria-label="Handoff done" value={String(action.config.done ?? "")} onChange={(event) => update("done", event.target.value)} /></label>
    <label className="grid gap-1 text-sm">Remaining<Textarea aria-label="Handoff remaining" value={String(action.config.remaining ?? "")} onChange={(event) => update("remaining", event.target.value)} /></label>
    <label className="grid gap-1 text-sm">Maximum Handoff depth<Input aria-label="Maximum Handoff depth" type="number" min={1} max={4} value={Number(action.config.max_depth ?? 2)} onChange={(event) => update("max_depth", Number(event.target.value))} /></label>
  </div>;
}

function panelTitle(step: HookStepKey) { return ({ trigger: "Choose the trigger", scope: "Choose what this applies to", filter: "Continue only if…", decision: "Choose the decision", action: "Choose the required action", failure: "Choose failure behavior" })[step]; }
function panelHelp(step: HookStepKey) { return ({ trigger: "Start with the event this hook should react to.", scope: "Choose named agents, issues, projects, workflows, or sessions without copying IDs.", filter: "Only fields available for the selected trigger can be chosen.", decision: "A block always wins when multiple policies match.", action: "Choose what happens and configure its named target.", failure: "Safety hooks can fail closed; ordinary automation should fail open." })[step]; }
function decisionDescription(decision: WorkflowHook["decision"]) { return ({ allow: "Continue without changing the proposed action.", block: "Stop and return a concrete remediation.", modify: "Change only fields declared mutable by the event.", require: "Require one or more outcomes before continuing." })[decision]; }

const ACTION_OPTIONS = [
  { value: "member.notify", label: "Notify member" }, { value: "agent.dispatch", label: "Start agent" }, { value: "squad.dispatch", label: "Start squad" },
  { value: "skill.run", label: "Run skill" }, { value: "judge.gate", label: "Judge gate" },
  { value: "wakeup.create", label: "Create wakeup" }, { value: "wakeup.cancel", label: "Cancel wakeup" }, { value: "session.handoff", label: "Start Handoff" },
  { value: "task.retry", label: "Repeat current step" }, { value: "task.cancel", label: "Cancel task" }, { value: "artifact.create_or_update", label: "Create or update artifact" },
  { value: "workflow.activate", label: "Start workflow" }, { value: "workflow.pause", label: "Pause workflow" }, { value: "workflow.resume", label: "Resume workflow" }, { value: "workflow.stop", label: "Stop workflow" },
  { value: "approval.require", label: "Require approval" }, { value: "audit.record", label: "Record audit event" }, { value: "metric.increment", label: "Increment metric" },
] as const;
