import { useState, type FormEvent } from "react";
import type { HorizonUnit, StrategyItem, StrategyKind, Terminology } from "../core/types";
import { useCreateStrategyItem, useUpdateStrategyItem } from "../core/queries";

interface StrategyFormProps { wsId: string; terminology: Terminology; position: number; item?: StrategyItem; onSaved: () => void; onCancel: () => void }

export function StrategyForm({ wsId, terminology, position, item, onSaved, onCancel }: StrategyFormProps) {
  const createItem = useCreateStrategyItem(wsId);
  const updateItem = useUpdateStrategyItem(wsId);
  const [kind, setKind] = useState<Exclude<StrategyKind, "unknown">>(item?.kind === "unknown" ? "horizon_goal" : item?.kind ?? "horizon_goal");
  const [title, setTitle] = useState(item?.title ?? "");
  const [description, setDescription] = useState(item?.description ?? "");
  const [count, setCount] = useState(item?.horizon_count ?? 1);
  const [unit, setUnit] = useState<HorizonUnit>(item?.horizon_unit ?? "year");
  const [label, setLabel] = useState(item?.horizon_label ?? "1-Year Plan");

  function submit(event: FormEvent) {
    event.preventDefault();
    const input = { kind, title: title.trim(), description: description.trim(), position: item?.position ?? position, state: item?.state === "unknown" ? "active" as const : item?.state ?? "active" as const, ...(kind === "horizon_goal" ? { horizon_count: count, horizon_unit: unit, horizon_label: label.trim() } : {}) };
    if (item) updateItem.mutate({ id: item.id, input }, { onSuccess: onSaved });
    else createItem.mutate(input, { onSuccess: onSaved });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-2">
      <div className="sm:col-span-2"><h3 className="font-semibold">{item ? "Edit" : "Add"} {terminology.strategy} item</h3></div>
      <label className="grid gap-1 text-sm">Type<select aria-label="Type" value={kind} onChange={(event) => setKind(event.target.value as Exclude<StrategyKind, "unknown">)} className="h-10 rounded-md border bg-background px-3"><option value="core_value">Core Value</option><option value="core_focus">Core Focus</option><option value="horizon_goal">Horizon goal</option></select></label>
      <label className="grid gap-1 text-sm">Title<input aria-label="Title" required value={title} onChange={(event) => setTitle(event.target.value)} className="h-10 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm sm:col-span-2">Description<textarea aria-label="Description" value={description} onChange={(event) => setDescription(event.target.value)} rows={3} className="rounded-md border bg-background px-3 py-2" /></label>
      {kind === "horizon_goal" && <><label className="grid gap-1 text-sm sm:col-span-2">Horizon name<input aria-label="Horizon name" required value={label} onChange={(event) => setLabel(event.target.value)} className="h-10 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm">Horizon count<input aria-label="Horizon count" type="number" min={1} value={count} onChange={(event) => setCount(Number(event.target.value))} className="h-10 rounded-md border bg-background px-3" /></label><label className="grid gap-1 text-sm">Horizon unit<select aria-label="Horizon unit" value={unit} onChange={(event) => setUnit(event.target.value as HorizonUnit)} className="h-10 rounded-md border bg-background px-3"><option value="day">Days</option><option value="week">Weeks</option><option value="month">Months</option><option value="year">Years</option></select></label></>}
      <div className="flex justify-end gap-2 sm:col-span-2"><button type="button" onClick={onCancel} className="h-10 rounded-md border px-4 text-sm font-medium">Cancel</button><button disabled={createItem.isPending || updateItem.isPending} className="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">Save {terminology.strategy} item</button></div>
    </form>
  );
}
