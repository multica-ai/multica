import { useState } from "react";
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
import { ACTION_CONFIGURATION, HOOK_ACTION_OPTIONS, fieldDefinition } from "../../core/hook-ux";
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
    <SelectTrigger aria-label={label} className="w-full overflow-hidden"><SelectValue><span className="truncate">{options.find((option) => option.value === value)?.label ?? placeholder}</span></SelectValue></SelectTrigger>
    <SelectContent>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
  </Select>;
}

export function HookStepPanel({ step, hook, onChange, directory = {} }: { step: HookStepKey; hook: WorkflowHook; onChange: (hook: WorkflowHook) => void; directory?: HookDirectory }) {
  const [showAdvanced, setShowAdvanced] = useState(hook.events.some((event) => HOOK_EVENT_OPTIONS.find((option) => option.value === event)?.advanced));
  const validation = validateHookStep(hook, step);
  const fields = fieldsForEvents(hook.events);
  const updateCondition = (index: number, patch: Partial<HookCondition>) => onChange({ ...hook, conditions: hook.conditions.map((condition, current) => current === index ? { ...condition, ...patch } : condition) });
  const updateAction = (index: number, type: string) => {
    const option = ACTION_OPTIONS.find((candidate) => candidate.value === type) ?? ACTION_OPTIONS[0];
    if (!option) return;
    onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { type: option.value, label: option.label, config: {} } : action) });
  };
  const updateActionConfig = (index: number, key: string, value: string | number | boolean) => onChange({ ...hook, actions: hook.actions.map((action, current) => current === index ? { ...action, config: { ...action.config, [key]: value } } : action) });

  const triggerSection = <div aria-label="Trigger" className="grid gap-3">
    <div><h3 className="text-sm font-semibold">Trigger</h3><p className="text-xs text-muted-foreground">The event this hook reacts to.</p></div>
    {hook.events.map((eventType, index) => <div key={`${eventType}-${index}`} className="flex items-end gap-2">
      <label className="grid flex-1 gap-1.5 text-sm font-medium">Trigger {index + 1}<ChoiceSelect label={`Trigger ${index + 1}`} value={eventType} options={HOOK_EVENT_OPTIONS.filter((option) => showAdvanced || !option.advanced)} onChange={(value) => onChange({ ...hook, events: hook.events.map((current, currentIndex) => currentIndex === index ? value as HookEventType : current) })} /></label>
      <Button type="button" variant="outline" size="icon" aria-label={`Remove trigger ${index + 1}`} onClick={() => onChange({ ...hook, events: hook.events.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
    </div>)}
    <div className="flex flex-wrap gap-2"><Button type="button" variant="outline" className="w-fit" aria-label="Add trigger" onClick={() => onChange({ ...hook, events: [...hook.events, "before.task.complete"] })}><Plus className="size-4" />Add trigger</Button><Button type="button" variant="ghost" className="w-fit" onClick={() => setShowAdvanced((current) => !current)}>{showAdvanced ? "Hide advanced events" : "Show advanced events"}</Button></div>
    {hook.events.length > 0 && <div className="grid gap-2">{hook.events.map((eventType, index) => <p key={`${eventType}-description-${index}`} className="rounded-md bg-muted/50 p-3 text-sm text-muted-foreground"><strong className="text-foreground">Trigger {index + 1}: </strong>{HOOK_EVENT_OPTIONS.find((option) => option.value === eventType)?.description}</p>)}</div>}
  </div>;

  const scopeSection = <div aria-label="Applies to" className="grid gap-3">
    <div><h3 className="text-sm font-semibold">Applies to</h3><p className="text-xs text-muted-foreground">Which agents, issues, projects, or sessions this watches. Keep This workspace to watch everything.</p></div>
    {hook.bindings.map((binding, index) => <div key={`${binding.kind}-${index}`} className="grid gap-2 rounded-lg border p-3 sm:grid-cols-[10rem_minmax(0,1fr)_auto] sm:items-end">
      <label className="grid gap-1.5 text-sm font-medium">Scope<ChoiceSelect label={`Scope ${index + 1}`} value={binding.kind} options={scopeOptions} onChange={(value) => onChange({ ...hook, bindings: hook.bindings.map((current, currentIndex) => currentIndex === index ? { kind: value as HookBinding["kind"], value: "" } : current) })} /></label>
      {binding.kind === "workspace" ? <p className="flex h-9 items-center text-sm text-muted-foreground">This workspace</p> : <label className="grid gap-1.5 text-sm font-medium">Named target<HookTargetPicker label={scopeOptions.find((option) => option.value === binding.kind)?.label ?? "Target"} value={binding.value} options={directory[binding.kind] ?? []} onSearch={binding.kind === "issue" ? directory.searchIssues : undefined} onChange={(value) => onChange({ ...hook, bindings: hook.bindings.map((current, currentIndex) => currentIndex === index ? { ...current, value } : current) })} /></label>}
      <Button type="button" variant="outline" size="icon" aria-label={`Remove scope ${index + 1}`} onClick={() => onChange({ ...hook, bindings: hook.bindings.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
    </div>)}
    <Button type="button" variant="outline" className="w-fit" aria-label="Add scope" onClick={() => onChange({ ...hook, bindings: [...hook.bindings, { kind: "workspace", value: "" }] })}><Plus className="size-4" />Add scope</Button>
  </div>;

  const filterSection = <div aria-label="Only when" className="grid gap-2">
    <div><h3 className="text-sm font-semibold">Only when <span className="font-normal text-muted-foreground">(optional)</span></h3><p className="text-xs text-muted-foreground">Extra conditions on the event's data. Leave empty to run every time.</p></div>
    {hook.conditions.length > 1 && <fieldset className="grid gap-1"><legend className="text-sm font-medium">Conditions that must match</legend><RadioGroup className="flex flex-wrap gap-2" value={hook.condition_mode} onValueChange={(value) => onChange({ ...hook, condition_mode: value as WorkflowHook["condition_mode"] })}>
      <label className={`flex min-h-11 items-center gap-2 rounded-lg border px-3 text-sm ${hook.condition_mode === "all" ? "border-primary bg-primary/10" : ""}`}><RadioGroupItem value="all" />All conditions</label>
      <label className={`flex min-h-11 items-center gap-2 rounded-lg border px-3 text-sm ${hook.condition_mode === "any" ? "border-primary bg-primary/10" : ""}`}><RadioGroupItem value="any" />Any condition</label>
    </RadioGroup></fieldset>}
    {hook.conditions.map((condition, index) => { const definition = fieldDefinition(condition.field); const rowError = conditionError(condition, fields); return <div key={index} aria-label={`Condition group ${index + 1}`} className="grid gap-2 rounded-xl border-l-4 border-l-primary/40 pl-2"><div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_9rem_minmax(0,1fr)_auto] sm:items-end">
      <label className="grid gap-1 text-sm">Field<ChoiceSelect label={`Filter field ${index + 1}`} value={condition.field} placeholder="Select field" options={fields.map((field) => ({ value: field, label: fieldDefinition(field).label }))} onChange={(value) => updateCondition(index, { field: value, value: "" })} /></label>
      <label className="grid gap-1 text-sm">Operator<ChoiceSelect label={`Filter operator ${index + 1}`} value={condition.operator} options={operatorOptions} onChange={(value) => updateCondition(index, { operator: value })} /></label>
      <label className="grid gap-1 text-sm">Value<ConditionValue index={index} condition={condition} definition={definition} onChange={(value) => updateCondition(index, { value })} /></label>
      <Button type="button" variant="outline" size="icon" aria-label={`Remove filter ${index + 1}`} onClick={() => onChange({ ...hook, conditions: hook.conditions.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button>
    </div>{rowError && <p role="alert" className="px-3 pb-2 text-xs text-destructive">{rowError}</p>}</div>})}
    <Button type="button" variant="outline" className="mt-2 w-fit" aria-label="Add filter" disabled={hook.events.length === 0} onClick={() => onChange({ ...hook, conditions: [...hook.conditions, { field: fields[0] ?? "", operator: "eq", value: "", conjunction: "AND" }] })}><Plus className="size-4" />Add filter</Button>
  </div>;

  const guideSection = <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">When this matches, the hook should</legend><RadioGroup value={hook.decision === "modify" ? "allow" : hook.decision} onValueChange={(value) => onChange({ ...hook, decision: value as WorkflowHook["decision"] })}>{(["allow", "require", "block"] as const).map((decision) => <div key={decision} className={`rounded-lg border p-3 ${hook.decision === decision ? "border-primary bg-primary/10" : ""}`}><label className="flex items-center gap-3 text-sm font-semibold"><RadioGroupItem value={decision} />{decisionLabel(decision)}</label><p className="ml-7 mt-1 text-xs text-muted-foreground">{decisionDescription(decision)}</p></div>)}</RadioGroup>
    {["block", "require"].includes(hook.decision) && <label className="mt-3 grid gap-1.5 text-sm font-medium">What the agent must do<Textarea aria-label="Required outcome" className="min-h-20" value={hook.requirement} onChange={(event) => onChange({ ...hook, requirement: event.target.value })} /></label>}
  </fieldset>;

  const actionsSection = <div className="grid gap-4">
    {hook.actions.map((action, index) => <div key={index} className="grid gap-3 rounded-lg border p-4">
      <div className="flex items-end gap-2"><label className="grid flex-1 gap-1.5 text-sm font-medium">Action {index + 1}<ChoiceSelect label={`Action ${index + 1} type`} value={action.type} options={ACTION_OPTIONS} onChange={(value) => updateAction(index, value)} /></label><Button type="button" variant="outline" size="icon" aria-label={`Remove action ${index + 1}`} onClick={() => onChange({ ...hook, actions: hook.actions.filter((_, current) => current !== index) })}><Trash2 className="size-4" /></Button></div>
      <p className="text-xs text-muted-foreground">{ACTION_CONFIGURATION[action.type]?.description}</p><ActionConfiguration action={action} directory={directory} update={(key, value) => updateActionConfig(index, key, value)} />
    </div>)}
    <Button type="button" variant="outline" className="w-fit" aria-label="Add action" onClick={() => onChange({ ...hook, actions: [...hook.actions, { type: "audit.record", label: "Record audit event", config: {} }] })}><Plus className="size-4" />Add action</Button>
    <fieldset aria-label="Advanced" className="mt-2 grid gap-2 border-t pt-4"><legend className="mb-2 text-sm font-medium">Advanced</legend><p className="-mt-1 mb-1 text-xs text-muted-foreground">If the check itself cannot run (a rare system error), what should happen?</p><RadioGroup value={hook.fail_mode === "open" ? "warn" : hook.fail_mode} onValueChange={(value) => onChange({ ...hook, fail_mode: value as WorkflowHook["fail_mode"] })}>{(["warn", "closed"] as const).map((mode) => <div key={mode} className="rounded-lg border p-3"><label className="flex items-center gap-3 text-sm font-semibold"><RadioGroupItem value={mode} />{failModeLabel(mode)}</label><p className="ml-7 mt-1 text-xs text-muted-foreground">{failModeDescription(mode)}</p></div>)}</RadioGroup></fieldset>
  </div>;

  return <section aria-label={`${step} configuration`} className="p-4 sm:p-6">
    <p className="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Configuration</p>
    <h2 className="mt-1 text-lg font-semibold">{panelTitle(step)}</h2>
    <p className="mt-1 text-sm text-muted-foreground">{panelHelp(step)}</p>
    {!validation.valid && <p role="alert" className="mb-5 mt-2 text-sm text-destructive">{validation.message}</p>}
    {validation.valid && <div className="mb-5" />}

    {step === "when" && <div className="grid gap-8">{triggerSection}{scopeSection}{filterSection}</div>}
    {step === "guide" && guideSection}
    {step === "actions" && actionsSection}
  </section>;
}

export const ACTION_OPTIONS = HOOK_ACTION_OPTIONS;

function ActionConfiguration({ action, directory, update }: { action: WorkflowHook["actions"][number]; directory: HookDirectory; update: (key: string, value: string | number | boolean) => void }) {
  const definition = ACTION_CONFIGURATION[action.type];
  if (!definition) return <p role="alert" className="text-sm text-destructive">This action is not configurable in the editor.</p>;
  return <div className="grid gap-3">{definition.fields.map((field) => <ActionField key={field.key} field={field} value={action.config[field.key]} directory={directory} update={(value) => update(field.key, value)} />)}</div>;
}

function ActionField({ field, value, directory, update }: { field: (typeof ACTION_CONFIGURATION)[string]["fields"][number]; value: unknown; directory: HookDirectory; update: (value: string | number | boolean) => void }) {
  const label = `${field.label}${field.required ? "" : " (optional)"}`;
  if (field.input === "target") return <label className="grid gap-1 text-sm">{label}<HookTargetPicker label={field.key === "target" ? "Handoff target" : field.key === "plan_ref" ? "Handoff plan reference" : field.label} value={String(value ?? "")} options={directory[field.target ?? "agent"] ?? []} onSearch={field.target === "issue" ? directory.searchIssues : undefined} onChange={update} />{field.help && <span className="text-xs text-muted-foreground">{field.help}</span>}</label>;
  if (field.input === "textarea") return <label className="grid gap-1 text-sm">{label}<Textarea aria-label={field.key === "rubric" ? "Judge rubric" : field.key === "summary" ? "Handoff summary" : field.key === "done" ? "Handoff done" : field.key === "remaining" ? "Handoff remaining" : field.label} value={String(value ?? "")} onChange={(event) => update(event.target.value)} /></label>;
  if (field.input === "checkbox") return <label className="flex items-center gap-2 text-sm"><Checkbox aria-label={field.label} checked={typeof value === "boolean" ? value : ("default" in field ? field.default : true) !== false} onCheckedChange={(checked) => update(checked === true)} />{field.label}{field.help && <span className="text-xs text-muted-foreground">{field.help}</span>}</label>;
  if (field.input === "select") return <label className="grid gap-1 text-sm">{label}<ChoiceSelect label={field.label} value={String(value ?? "")} options={field.options ?? []} onChange={update} /></label>;
  const displayedValue = field.input === "number" ? Number(value ?? (field.key === "max_depth" ? 2 : 1)) : field.input === "datetime-local" ? toLocalDateTime(String(value ?? "")) : String(value ?? "");
  return <label className="grid gap-1 text-sm">{label}<Input aria-label={field.key === "plan_ref" ? "Handoff plan reference" : field.label} type={field.input} min={field.key === "max_depth" ? 1 : undefined} max={field.key === "max_depth" ? 4 : undefined} value={displayedValue} onChange={(event) => update(field.input === "number" ? Number(event.target.value) : field.input === "datetime-local" && event.target.value ? new Date(event.target.value).toISOString() : event.target.value)} />{field.help && <span className="text-xs text-muted-foreground">{field.help}</span>}</label>;
}

function toLocalDateTime(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function ConditionValue({ index, condition, definition, onChange }: { index: number; condition: HookCondition; definition: ReturnType<typeof fieldDefinition>; onChange: (value: string) => void }) {
  const disabled = ["exists", "not_exists"].includes(condition.operator);
  if (disabled) return <Input aria-label={`Filter value ${index + 1}`} value="No value needed" disabled />;
  if (definition.input === "select" && condition.operator !== "not_in") return <ChoiceSelect label={`Filter value ${index + 1}`} value={condition.value} options={definition.options ?? []} onChange={onChange} />;
  if (definition.input === "boolean") return <ChoiceSelect label={`Filter value ${index + 1}`} value={condition.value} options={[{ value: "true", label: "Yes" }, { value: "false", label: "No" }]} onChange={onChange} />;
  return <Input aria-label={`Filter value ${index + 1}`} type={definition.input === "select" ? "text" : definition.input} value={condition.value} onChange={(event) => onChange(event.target.value)} />;
}

function conditionError(condition: HookCondition, fields: string[]) {
  if (!fields.includes(condition.field)) return "Choose a field available for this trigger.";
  if (!condition.operator) return "Choose an operator.";
  if (!["exists", "not_exists"].includes(condition.operator) && !condition.value.trim()) return "Choose or enter a value.";
  return "";
}

function panelTitle(step: HookStepKey) { return ({ when: "When this runs", guide: "Guide or enforce", actions: "Actions" })[step]; }
function panelHelp(step: HookStepKey) { return ({ when: "Pick the event, who it applies to, and any conditions that must match.", guide: "Choose whether to guide the agent, require an outcome, or stop the action.", actions: "Choose what happens when this hook matches." })[step]; }
function decisionLabel(decision: WorkflowHook["decision"]) { return ({ allow: "Guide", block: "Stop", modify: "Modify", require: "Require an outcome" })[decision]; }
function decisionDescription(decision: WorkflowHook["decision"]) { return ({ allow: "Let the action continue. Use the actions below to add guidance, reminders, or logging without stopping the agent.", block: "Stop the action and tell the agent what must change.", modify: "Change only fields declared mutable by the event.", require: "Let it continue only after the agent satisfies a stated outcome." })[decision]; }
function failModeLabel(mode: WorkflowHook["fail_mode"]) { return ({ open: "Continue", closed: "Stop", warn: "Continue and log" })[mode]; }
function failModeDescription(mode: WorkflowHook["fail_mode"]) { return ({ open: "Let the action continue.", closed: "Stop the action to be safe.", warn: "Let the action continue and record that the check could not run." })[mode]; }
