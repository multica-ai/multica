"use client";

import { useEffect, useState, type ComponentType, type FormEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Settings2 } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { DEFAULT_TERMINOLOGY } from "../core/api-schemas";
import { elementsOptions, goalTypesOptions, periodsOptions, settingsOptions, useDeleteGoalType, useDeletePeriod, useSaveGoalType, useSavePeriod, useUpdateElement, useUpdateSettings } from "../core/queries";
import type { GoalType, OperatingPeriod, PeriodUnit, Terminology } from "../core/types";

interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  content: ReactNode;
  wide?: boolean;
}

const STANDARD_TERMINOLOGY: Terminology = { ...DEFAULT_TERMINOLOGY };

const TERMINOLOGY_FIELDS: Array<{ key: keyof Terminology; label: string }> = [
  { key: "strategy", label: "Strategy label" },
  { key: "rock", label: "Goal label (singular)" },
  { key: "rocks", label: "Goals label (plural)" },
  { key: "vision_plan", label: "Vision Plan label" },
  { key: "meetings", label: "Meetings label" },
  { key: "org_chart", label: "Org Chart label" },
  { key: "scorecard", label: "Scorecard label" },
  { key: "issues_list", label: "Issues List label" },
  { key: "strategy_map", label: "Strategy Map label" },
];

function isStandard(values: Terminology) {
  return TERMINOLOGY_FIELDS.every(({ key }) => values[key] === STANDARD_TERMINOLOGY[key]);
}

function elementLabel(key: string, terminology: Terminology) {
  switch (key) {
    case "goals": return terminology.rocks;
    case "vision_plan": return terminology.vision_plan;
    case "meetings": return terminology.meetings;
    case "org_chart": return terminology.org_chart;
    case "scorecard": return terminology.scorecard;
    case "issues_list": return terminology.issues_list;
    case "strategy_map": return terminology.strategy_map;
    default: return key;
  }
}

export function OperatingSystemSettingsTab() {
  const wsId = useWorkspaceId();
  const settings = useQuery(settingsOptions(wsId));
  const terminology = settings.data?.terminology ?? STANDARD_TERMINOLOGY;

  return (
    <div className="mx-auto grid max-w-3xl gap-8">
      <div><h2 className="text-xl font-semibold">Operating System</h2><p className="mt-1 text-sm text-muted-foreground">Choose which elements your workspace uses and adapt the language to your company. Data relationships stay unchanged.</p></div>
      <ElementsSection wsId={wsId} terminology={terminology} />
      <GoalTypesSection wsId={wsId} terminology={terminology} />
      <PeriodsSection wsId={wsId} />
      <TerminologySection wsId={wsId} terminology={terminology} />
    </div>
  );
}

function ElementsSection({ wsId, terminology }: { wsId: string; terminology: Terminology }) {
  const elements = useQuery(elementsOptions(wsId));
  const update = useUpdateElement(wsId);
  return (
    <section className="grid gap-3">
      <div><h3 className="font-semibold">Elements</h3><p className="text-sm text-muted-foreground">Turn each Operating System element on or off for this workspace.</p></div>
      <div className="grid gap-1 rounded-xl border bg-card p-2">
        {(elements.data?.elements ?? []).map((element) => (
          <label key={element.key} className="flex items-center justify-between gap-3 rounded-md px-3 py-2 text-sm hover:bg-muted/40">
            <span>{elementLabel(element.key, terminology)}</span>
            <input aria-label={`${elementLabel(element.key, terminology)} enabled`} type="checkbox" checked={element.enabled} onChange={(event) => update.mutate({ key: element.key, enabled: event.target.checked })} className="h-4 w-4" />
          </label>
        ))}
        {elements.isLoading && <p className="px-3 py-2 text-sm text-muted-foreground">Loading elements…</p>}
        {elements.isError && <p role="alert" className="px-3 py-2 text-sm text-destructive">Elements could not be loaded.</p>}
      </div>
      {update.isError && <p role="alert" className="text-sm text-destructive">The element could not be saved.</p>}
    </section>
  );
}

function GoalTypesSection({ wsId, terminology }: { wsId: string; terminology: Terminology }) {
  const goalTypes = useQuery(goalTypesOptions(wsId));
  const save = useSaveGoalType(wsId);
  const remove = useDeleteGoalType(wsId);
  const [editing, setEditing] = useState<GoalType | null | undefined>(undefined);

  return (
    <section className="grid gap-3">
      <div className="flex items-end justify-between gap-3">
        <div><h3 className="font-semibold">{terminology.rock} types</h3><p className="text-sm text-muted-foreground">Define the kinds of {terminology.rocks.toLowerCase()} your workspace tracks, each with its own color and scope.</p></div>
        <button type="button" onClick={() => setEditing(null)} className="h-9 rounded-md border px-3 text-sm font-medium">+ New type</button>
      </div>
      <div className="grid gap-1 rounded-xl border bg-card p-2">
        {(goalTypes.data?.goal_types ?? []).map((goalType) => (
          <div key={goalType.id} className="flex items-center justify-between gap-3 rounded-md px-3 py-2 text-sm hover:bg-muted/40">
            <span className="flex min-w-0 items-center gap-2">
              <span aria-hidden className="h-3 w-3 shrink-0 rounded-full" style={{ backgroundColor: goalType.color }} />
              <strong className="truncate">{goalType.name}</strong>
              {goalType.scope_label && <span className="truncate text-xs text-muted-foreground">{goalType.scope_label}</span>}
            </span>
            <span className="flex shrink-0 gap-2">
              <button type="button" onClick={() => setEditing(goalType)} className="text-xs font-medium underline-offset-2 hover:underline">Edit</button>
              <button type="button" aria-label={`Delete ${goalType.name}`} onClick={() => remove.mutate(goalType.id)} className="text-xs font-medium text-destructive underline-offset-2 hover:underline">Delete</button>
            </span>
          </div>
        ))}
        {!goalTypes.isLoading && (goalTypes.data?.goal_types ?? []).length === 0 && <p className="px-3 py-2 text-sm text-muted-foreground">No types yet. Create the first one to badge and filter {terminology.rocks.toLowerCase()}.</p>}
      </div>
      {editing !== undefined && <GoalTypeForm key={editing?.id ?? "new"} goalType={editing ?? undefined} pending={save.isPending} error={save.isError} onSave={(input) => save.mutate({ id: editing?.id, input }, { onSuccess: () => setEditing(undefined) })} onCancel={() => setEditing(undefined)} />}
    </section>
  );
}

function GoalTypeForm({ goalType, pending, error, onSave, onCancel }: { goalType?: GoalType; pending: boolean; error: boolean; onSave: (input: { name: string; color: string; scope_label: string }) => void; onCancel: () => void }) {
  const [name, setName] = useState(goalType?.name ?? "");
  const [color, setColor] = useState(goalType?.color || "#6366F1");
  const [scopeLabel, setScopeLabel] = useState(goalType?.scope_label ?? "");

  function submit(event: FormEvent) {
    event.preventDefault();
    onSave({ name: name.trim(), color, scope_label: scopeLabel.trim() });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-4 sm:grid-cols-3">
      <label className="grid gap-1 text-sm">Type name<input aria-label="Type name" required value={name} onChange={(event) => setName(event.target.value)} className="h-10 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm">Color<input aria-label="Color" type="color" value={color} onChange={(event) => setColor(event.target.value)} className="h-10 w-full rounded-md border bg-background px-1" /></label>
      <label className="grid gap-1 text-sm">Scope label<input aria-label="Scope label" value={scopeLabel} onChange={(event) => setScopeLabel(event.target.value)} placeholder="e.g. company-wide" className="h-10 rounded-md border bg-background px-3" /></label>
      {error && <p role="alert" className="text-sm text-destructive sm:col-span-3">The type could not be saved.</p>}
      <div className="flex justify-end gap-2 sm:col-span-3">
        <button type="button" onClick={onCancel} className="h-9 rounded-md border px-3 text-sm font-medium">Cancel</button>
        <button disabled={pending} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Save type</button>
      </div>
    </form>
  );
}

function nextPeriodStart(periods: OperatingPeriod[]): string {
  const latest = periods.map((period) => period.ends_on).sort().at(-1);
  if (!latest) return new Date().toISOString().slice(0, 10);
  const next = new Date(`${latest}T00:00:00Z`);
  next.setUTCDate(next.getUTCDate() + 1);
  return next.toISOString().slice(0, 10);
}

function PeriodsSection({ wsId }: { wsId: string }) {
  const periods = useQuery(periodsOptions(wsId));
  const save = useSavePeriod(wsId);
  const remove = useDeletePeriod(wsId);
  const [adding, setAdding] = useState(false);
  const [unit, setUnit] = useState<PeriodUnit>("quarter");
  const [name, setName] = useState("");
  const [startsOn, setStartsOn] = useState("");
  const [endsOn, setEndsOn] = useState("");

  const list = periods.data?.periods ?? [];

  function openForm() {
    setStartsOn(nextPeriodStart(list));
    setName("");
    setEndsOn("");
    setAdding(true);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate({ input: { unit, name: name.trim() || undefined, starts_on: startsOn, ends_on: unit === "custom" ? endsOn : undefined } }, { onSuccess: () => setAdding(false) });
  }

  return (
    <section className="grid gap-3">
      <div className="flex items-end justify-between gap-3">
        <div><h3 className="font-semibold">Periods</h3><p className="text-sm text-muted-foreground">Define the cadence goals are planned in — monthly, quarterly, or custom — and plan periods ahead of time.</p></div>
        <button type="button" onClick={openForm} className="h-9 rounded-md border px-3 text-sm font-medium">+ New period</button>
      </div>
      <div className="grid gap-1 rounded-xl border bg-card p-2">
        {list.map((period) => (
          <div key={period.id} className="flex items-center justify-between gap-3 rounded-md px-3 py-2 text-sm hover:bg-muted/40">
            <span className="flex min-w-0 items-center gap-2">
              <strong className="truncate">{period.name}</strong>
              <span className="shrink-0 rounded-full border px-2 py-0.5 text-xs text-muted-foreground">{period.unit}</span>
              <span className="truncate text-xs text-muted-foreground">{period.starts_on} – {period.ends_on}</span>
            </span>
            <button type="button" aria-label={`Delete ${period.name}`} onClick={() => remove.mutate(period.id)} className="shrink-0 text-xs font-medium text-destructive underline-offset-2 hover:underline">Delete</button>
          </div>
        ))}
        {!periods.isLoading && list.length === 0 && <p className="px-3 py-2 text-sm text-muted-foreground">No periods yet.</p>}
      </div>
      {remove.isError && <p role="alert" className="text-sm text-destructive">The period could not be deleted. Periods with goals in them must be emptied first.</p>}
      {adding && (
        <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-4 sm:grid-cols-2">
          <label className="grid gap-1 text-sm">Period unit
            <select aria-label="Period unit" value={unit} onChange={(event) => setUnit(event.target.value as PeriodUnit)} className="h-10 rounded-md border bg-background px-3">
              <option value="month">Month</option>
              <option value="quarter">Quarter</option>
              <option value="custom">Custom range</option>
            </select>
          </label>
          <label className="grid gap-1 text-sm">Starts on<input aria-label="Starts on" type="date" required value={startsOn} onChange={(event) => setStartsOn(event.target.value)} className="h-10 rounded-md border bg-background px-3" /></label>
          {unit === "custom" && <label className="grid gap-1 text-sm">Ends on<input aria-label="Ends on" type="date" required value={endsOn} onChange={(event) => setEndsOn(event.target.value)} className="h-10 rounded-md border bg-background px-3" /></label>}
          <label className="grid gap-1 text-sm">Name {unit !== "custom" && <span className="text-xs text-muted-foreground">Optional · generated from the dates</span>}
            <input aria-label="Period name" required={unit === "custom"} value={name} onChange={(event) => setName(event.target.value)} className="h-10 rounded-md border bg-background px-3" />
          </label>
          {save.isError && <p role="alert" className="text-sm text-destructive sm:col-span-2">The period could not be saved.</p>}
          <div className="flex justify-end gap-2 sm:col-span-2">
            <button type="button" onClick={() => setAdding(false)} className="h-9 rounded-md border px-3 text-sm font-medium">Cancel</button>
            <button disabled={save.isPending} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Save period</button>
          </div>
        </form>
      )}
    </section>
  );
}

function TerminologySection({ wsId, terminology }: { wsId: string; terminology: Terminology }) {
  const update = useUpdateSettings(wsId);
  const [values, setValues] = useState<Terminology>(terminology);
  const [profile, setProfile] = useState(isStandard(terminology) ? "standard" : "custom");
  const terminologyFingerprint = TERMINOLOGY_FIELDS.map(({ key }) => terminology[key]).join(" ");

  useEffect(() => {
    setValues(terminology);
    setProfile(isStandard(terminology) ? "standard" : "custom");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [terminologyFingerprint]);

  function changeProfile(next: string) {
    setProfile(next);
    if (next === "standard") setValues(STANDARD_TERMINOLOGY);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate(values, { onSuccess: () => setProfile(isStandard(values) ? "standard" : "custom") });
  }

  return (
    <section className="grid gap-3">
      <div><h3 className="font-semibold">Terminology</h3><p className="text-sm text-muted-foreground">Rename every element to match the language your company already uses.</p></div>
      <form onSubmit={submit} className="grid gap-5 rounded-xl border bg-card p-5 sm:grid-cols-2">
        <label className="grid gap-1 text-sm sm:col-span-2">Profile
          <select aria-label="Profile" value={profile} onChange={(event) => changeProfile(event.target.value)} className="h-10 rounded-md border bg-background px-3"><option value="standard">Standard</option><option value="custom">Custom</option></select>
        </label>
        {TERMINOLOGY_FIELDS.map(({ key, label }) => (
          <label key={key} className="grid gap-1 text-sm">{label}
            <input aria-label={label} required value={values[key]} onChange={(event) => { setValues({ ...values, [key]: event.target.value }); setProfile("custom"); }} className="h-10 rounded-md border bg-background px-3" />
          </label>
        ))}
        {update.isError && <p role="alert" className="text-sm text-destructive sm:col-span-2">Terminology could not be saved.</p>}
        <div className="flex justify-end sm:col-span-2"><button disabled={update.isPending} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save terminology</button></div>
      </form>
    </section>
  );
}

export function useCerebroOperatingSystemSettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_operating_system");
  return enabled ? [{ value: "operating-system", label: "Operating System", icon: Settings2, content: <OperatingSystemSettingsTab /> }] : [];
}
