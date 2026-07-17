"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { orgChartOptions, useCreateOrgChartSeat, useDeleteOrgChartSeat, useUpdateOrgChartSeat } from "../core/queries";
import type { OrgChartSeat, OrgChartSeatInput } from "../core/types";
import { SearchSelect } from "./search-select";

const ownerOptions = [{ value: "member", label: "Human member" }, { value: "agent", label: "Agent" }];

function inputFromSeat(seat: OrgChartSeat): OrgChartSeatInput {
  return { parent_id: seat.parent_id, name: seat.name, responsibilities: seat.responsibilities, owner_type: seat.owner_type, owner_id: seat.owner_id, position: seat.position };
}

function SeatEditor({ seat, seats, onSave, onDelete, pending }: { seat?: OrgChartSeat; seats: OrgChartSeat[]; onSave: (input: OrgChartSeatInput) => void; onDelete?: () => void; pending: boolean }) {
  const initial = seat ? inputFromSeat(seat) : { name: "", responsibilities: [], position: seats.length };
  const [draft, setDraft] = useState<OrgChartSeatInput>(initial);
  const parentOptions = seats.filter((candidate) => candidate.id !== seat?.id).map((candidate) => ({ value: candidate.id, label: candidate.name }));
  return <div className="grid gap-3 rounded-xl border bg-card p-4">
    <div className="grid gap-3 md:grid-cols-[1fr_12rem_12rem]">
      <label className="grid gap-1 text-sm">Seat name<input aria-label="Seat name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="e.g. Operations" className="h-10 rounded-md border bg-background px-3" /></label>
      <label className="grid gap-1 text-sm">Reports to<SearchSelect label="Reports to" compact options={parentOptions} value={draft.parent_id ?? ""} onChange={(value) => setDraft({ ...draft, parent_id: value || undefined })} clearLabel="No parent" placeholder="Select parent" /></label>
      <label className="grid gap-1 text-sm">Owner type<SearchSelect label="Owner type" compact options={ownerOptions} value={draft.owner_type ?? ""} onChange={(value) => setDraft({ ...draft, owner_type: (value || undefined) as OrgChartSeatInput["owner_type"], owner_id: value ? draft.owner_id : undefined })} clearLabel="Vacant" placeholder="Choose owner type" /></label>
    </div>
    <label className="grid gap-1 text-sm">Responsibilities <span className="text-xs text-muted-foreground">One responsibility per line</span><textarea aria-label="Responsibilities" value={draft.responsibilities.join("\n")} onChange={(event) => setDraft({ ...draft, responsibilities: event.target.value.split("\n") })} rows={3} className="rounded-md border bg-background p-3" /></label>
    {draft.owner_type && <label className="grid gap-1 text-sm">Owner ID<input aria-label="Owner ID" value={draft.owner_id ?? ""} onChange={(event) => setDraft({ ...draft, owner_id: event.target.value })} placeholder="UUID of the member or agent" className="h-10 rounded-md border bg-background px-3" /></label>}
    <div className="flex justify-end gap-2"><button type="button" disabled={pending} onClick={() => onSave({ ...draft, name: draft.name.trim(), responsibilities: draft.responsibilities.map((value) => value.trim()).filter(Boolean) })} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">Save seat</button>{onDelete && <button type="button" disabled={pending} onClick={onDelete} className="h-9 rounded-md border px-3 text-sm font-medium text-destructive">Delete</button>}</div>
  </div>;
}

export function OrgChartPage() {
  const wsId = useWorkspaceId();
  const chart = useQuery(orgChartOptions(wsId));
  const create = useCreateOrgChartSeat(wsId);
  const update = useUpdateOrgChartSeat(wsId);
  const remove = useDeleteOrgChartSeat(wsId);
  const [adding, setAdding] = useState(false);
  const seats = chart.data?.seats ?? [];
  if (chart.isLoading) return <div className="mx-auto max-w-4xl p-6 text-sm text-muted-foreground">Loading org chart…</div>;
  if (chart.isError) return <div role="alert" className="mx-auto max-w-4xl p-6 text-sm text-destructive">Org chart could not be loaded.</div>;
  return <div className="mx-auto grid max-w-4xl gap-6 p-6"><div className="flex items-end justify-between gap-3"><div><h1 className="text-2xl font-semibold">Org Chart</h1><p className="mt-1 text-sm text-muted-foreground">Define seats, responsibilities and ownership. A seat without an owner stays visible as vacant.</p></div><button type="button" onClick={() => setAdding(true)} className="h-9 rounded-md border px-3 text-sm font-medium">+ Add seat</button></div>
    {adding && <SeatEditor seats={seats} pending={create.isPending} onSave={(input) => create.mutate(input, { onSuccess: () => setAdding(false) })} />}
    <div className="grid gap-3">{seats.map((seat) => <SeatRow key={seat.id} seat={seat} seats={seats} pending={update.isPending || remove.isPending} onSave={(input) => update.mutate({ id: seat.id, input })} onDelete={() => remove.mutate(seat.id)} />)}{seats.length === 0 && !adding && <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">No seats yet. Add the first seat to make ownership visible.</div>}</div>
    {(create.isError || update.isError || remove.isError) && <p role="alert" className="text-sm text-destructive">The org chart change could not be saved.</p>}
  </div>;
}

function SeatRow({ seat, seats, pending, onSave, onDelete }: { seat: OrgChartSeat; seats: OrgChartSeat[]; pending: boolean; onSave: (input: OrgChartSeatInput) => void; onDelete: () => void }) {
  return <div className="md:pl-4"><SeatEditor seat={seat} seats={seats} pending={pending} onSave={onSave} onDelete={onDelete} /></div>;
}
