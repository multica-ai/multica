"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { orgChartOptions, useCreateOrgChartSeat, useDeleteOrgChartSeat, useUpdateOrgChartSeat } from "../core/queries";
import type { OrgChartSeat, OrgChartSeatInput } from "../core/types";
import { SearchSelect, type SearchSelectOption } from "./search-select";

function inputFromSeat(seat: OrgChartSeat): OrgChartSeatInput {
  return { parent_id: seat.parent_id, name: seat.name, responsibilities: seat.responsibilities, owner_type: seat.owner_type, owner_id: seat.owner_id, position: seat.position };
}

// The owner picker speaks names, never IDs: one searchable field lists the
// workspace's people and agents, and "Vacant" is simply no selection.
function ownerValue(seat: { owner_type?: "member" | "agent"; owner_id?: string }) {
  return seat.owner_id ? `${seat.owner_type}:${seat.owner_id}` : "";
}

function SeatEditor({ seat, seats, ownerOptions, onSave, onCancel, onDelete, pending }: {
  seat?: OrgChartSeat;
  seats: OrgChartSeat[];
  ownerOptions: SearchSelectOption[];
  onSave: (input: OrgChartSeatInput) => void;
  onCancel: () => void;
  onDelete?: () => void;
  pending: boolean;
}) {
  const [draft, setDraft] = useState<OrgChartSeatInput>(seat ? inputFromSeat(seat) : { name: "", responsibilities: [], position: seats.length });
  const parentOptions = seats.filter((candidate) => candidate.id !== seat?.id).map((candidate) => ({ value: candidate.id, label: candidate.name }));

  function setResponsibility(index: number, value: string) {
    setDraft({ ...draft, responsibilities: draft.responsibilities.map((item, current) => current === index ? value : item) });
  }

  return (
    <div className="grid gap-3 rounded-xl border bg-card p-4">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm font-medium">Seat name
          <input aria-label="Seat name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="e.g. Operations" className="h-10 rounded-md border bg-background px-3" />
        </label>
        <label className="grid gap-1 text-sm font-medium">Reports to
          <SearchSelect label="Reports to" compact options={parentOptions} value={draft.parent_id ?? ""} onChange={(value) => setDraft({ ...draft, parent_id: value || undefined })} clearLabel="No parent (top of chart)" placeholder="Select a seat" />
        </label>
      </div>

      <label className="grid gap-1 text-sm font-medium">Owner
        <SearchSelect
          label="Owner"
          compact
          options={ownerOptions}
          value={ownerValue(draft)}
          onChange={(value) => {
            const [ownerType, ownerId] = value.split(":");
            setDraft({ ...draft, owner_type: ownerId ? (ownerType as OrgChartSeatInput["owner_type"]) : undefined, owner_id: ownerId || undefined });
          }}
          clearLabel="Leave vacant"
          placeholder="Search people and agents"
        />
      </label>

      <div className="grid gap-1 text-sm font-medium">Responsibilities
        <div className="grid gap-2">
          {draft.responsibilities.map((responsibility, index) => (
            <div key={index} className="flex items-center gap-2">
              <input
                aria-label={`Responsibility ${index + 1}`}
                value={responsibility}
                onChange={(event) => setResponsibility(index, event.target.value)}
                placeholder="What this seat is accountable for"
                className="h-9 flex-1 rounded-md border bg-background px-3 font-normal"
              />
              <button type="button" aria-label={`Remove responsibility ${index + 1}`} onClick={() => setDraft({ ...draft, responsibilities: draft.responsibilities.filter((_, current) => current !== index) })} className="h-9 rounded-md border px-2 text-xs text-muted-foreground hover:bg-muted">Remove</button>
            </div>
          ))}
          <button type="button" onClick={() => setDraft({ ...draft, responsibilities: [...draft.responsibilities, ""] })} className="h-9 w-fit rounded-md border border-dashed px-3 text-xs font-medium text-muted-foreground hover:bg-muted">+ Add responsibility</button>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="h-9 rounded-md border px-3 text-sm font-medium">Cancel</button>
        {onDelete && <button type="button" disabled={pending} onClick={onDelete} className="h-9 rounded-md border px-3 text-sm font-medium text-destructive">Delete seat</button>}
        <button type="button" disabled={pending || !draft.name.trim()} onClick={() => onSave({ ...draft, name: draft.name.trim(), responsibilities: draft.responsibilities.map((value) => value.trim()).filter(Boolean) })} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50">Save seat</button>
      </div>
    </div>
  );
}

function SeatCard({ seat, childCount, onEdit }: { seat: OrgChartSeat; childCount: number; onEdit: () => void }) {
  return (
    <div className="rounded-xl border bg-card p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="truncate text-sm font-semibold">{seat.name}</h3>
          <p className="mt-0.5 text-xs">
            {seat.owner_name
              ? <span className="text-muted-foreground">{seat.owner_type === "agent" ? "Agent" : "Owner"}: <span className="font-medium text-foreground">{seat.owner_name}</span></span>
              : <span className="rounded-full bg-amber-100 px-2 py-0.5 font-medium text-amber-800 dark:bg-amber-950 dark:text-amber-300">Vacant</span>}
          </p>
        </div>
        <button type="button" aria-label={`Edit ${seat.name}`} onClick={onEdit} className="shrink-0 rounded-md border px-2 py-1 text-xs font-medium hover:bg-muted">Edit</button>
      </div>
      {seat.responsibilities.length > 0 && (
        <ul className="mt-2 grid list-disc gap-0.5 pl-5 text-xs text-muted-foreground">
          {seat.responsibilities.map((responsibility, index) => <li key={index}>{responsibility}</li>)}
        </ul>
      )}
      {childCount > 0 && <p className="mt-2 text-[11px] uppercase tracking-wide text-muted-foreground">{childCount} direct {childCount === 1 ? "report" : "reports"}</p>}
    </div>
  );
}

export function OrgChartPage() {
  const wsId = useWorkspaceId();
  const chart = useQuery(orgChartOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const create = useCreateOrgChartSeat(wsId);
  const update = useUpdateOrgChartSeat(wsId);
  const remove = useDeleteOrgChartSeat(wsId);
  const [adding, setAdding] = useState(false);
  const [editingId, setEditingId] = useState<string>();

  const seats = chart.data?.seats ?? [];
  const ownerOptions: SearchSelectOption[] = [
    ...(members.data ?? []).map((member) => ({ value: `member:${member.id}`, label: member.name, group: "People" })),
    ...(agents.data ?? []).map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];

  const childrenOf = (parentId?: string) => seats
    .filter((seat) => (seat.parent_id ?? undefined) === parentId)
    .sort((a, b) => a.position - b.position);

  if (chart.isLoading) return <div className="mx-auto max-w-5xl p-6 text-sm text-muted-foreground">Loading org chart…</div>;
  if (chart.isError) return <div role="alert" className="mx-auto max-w-5xl p-6 text-sm text-destructive">Org chart could not be loaded.</div>;

  function renderSeat(seat: OrgChartSeat, depth: number) {
    const children = childrenOf(seat.id);
    return (
      <div key={seat.id} className="grid gap-3" style={{ marginLeft: depth > 0 ? 20 : 0 }}>
        <div className={depth > 0 ? "border-l-2 border-dashed pl-4" : ""}>
          {editingId === seat.id
            ? <SeatEditor seat={seat} seats={seats} ownerOptions={ownerOptions} pending={update.isPending || remove.isPending} onCancel={() => setEditingId(undefined)} onSave={(input) => update.mutate({ id: seat.id, input }, { onSuccess: () => setEditingId(undefined) })} onDelete={() => remove.mutate(seat.id, { onSuccess: () => setEditingId(undefined) })} />
            : <SeatCard seat={seat} childCount={children.length} onEdit={() => setEditingId(seat.id)} />}
        </div>
        {children.map((child) => renderSeat(child, depth + 1))}
      </div>
    );
  }

  const roots = childrenOf(undefined);

  return (
    <div className="mx-auto grid max-w-5xl gap-6 p-6">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Org Chart</h1>
          <p className="mt-1 text-sm text-muted-foreground">Seats reporting into each other. Pick an owner by name; an empty seat stays visible as vacant.</p>
        </div>
        <button type="button" onClick={() => { setAdding(true); setEditingId(undefined); }} className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">+ Add seat</button>
      </div>

      {adding && <SeatEditor seats={seats} ownerOptions={ownerOptions} pending={create.isPending} onCancel={() => setAdding(false)} onSave={(input) => create.mutate(input, { onSuccess: () => setAdding(false) })} />}

      {roots.length === 0 && !adding
        ? <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">No seats yet. Add the first seat to make ownership visible.</div>
        : <div className="grid gap-3">{roots.map((seat) => renderSeat(seat, 0))}</div>}

      {(create.isError || update.isError || remove.isError) && <p role="alert" className="text-sm text-destructive">The org chart change could not be saved.</p>}
    </div>
  );
}
