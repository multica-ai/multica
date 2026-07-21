"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { orgChartOptions, settingsOptions, useCreateOrgChartSeat, useDeleteOrgChartSeat, useUpdateOrgChartSeat } from "../core/queries";
import type { OrgChartSeat, OrgChartSeatInput } from "../core/types";
import { SearchSelect, type SearchSelectOption } from "./search-select";
import { RolesChart, type InsertMode, type SeatActor } from "./roles-chart";

function inputFromSeat(seat: OrgChartSeat): OrgChartSeatInput {
  return { parent_id: seat.parent_id, name: seat.name, responsibilities: seat.responsibilities, owner_type: seat.owner_type, owner_id: seat.owner_id, position: seat.position };
}

function initialsOf(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase() ?? "").join("") || "?";
}

// Where a new seat lands relative to the clicked one: "beside" is a sibling,
// "below" a direct report, and "above" a new parent the clicked seat moves under.
interface InsertIntent { relativeToId: string; mode: InsertMode }

// The owner picker speaks names, never IDs: one searchable field lists the
// workspace's people and agents, and "Vacant" is simply no selection.
function ownerValue(seat: { owner_type?: "member" | "agent"; owner_id?: string }) {
  return seat.owner_id ? `${seat.owner_type}:${seat.owner_id}` : "";
}

function SeatEditor({ seat, seats, ownerOptions, presetParentId, onSave, onCancel, onDelete, pending }: {
  seat?: OrgChartSeat;
  seats: OrgChartSeat[];
  ownerOptions: SearchSelectOption[];
  presetParentId?: string;
  onSave: (input: OrgChartSeatInput) => void;
  onCancel: () => void;
  onDelete?: () => void;
  pending: boolean;
}) {
  const [draft, setDraft] = useState<OrgChartSeatInput>(seat ? inputFromSeat(seat) : { name: "", responsibilities: [], position: seats.length, parent_id: presetParentId });
  const parentOptions = seats.filter((candidate) => candidate.id !== seat?.id).map((candidate) => ({ value: candidate.id, label: candidate.name }));

  function setResponsibility(index: number, value: string) {
    setDraft({ ...draft, responsibilities: draft.responsibilities.map((item, current) => current === index ? value : item) });
  }

  return (
    <div className="grid gap-3 rounded-xl border bg-card p-4">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm font-medium">Seat name
          <input aria-label="Seat name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="e.g. Operations" className="min-h-11 rounded-md border bg-background px-3" />
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
                className="min-h-11 flex-1 rounded-md border bg-background px-3 font-normal"
              />
              <button type="button" aria-label={`Remove responsibility ${index + 1}`} onClick={() => setDraft({ ...draft, responsibilities: draft.responsibilities.filter((_, current) => current !== index) })} className="min-h-11 rounded-md border px-3 text-xs text-muted-foreground hover:bg-muted">Remove</button>
            </div>
          ))}
          <button type="button" onClick={() => setDraft({ ...draft, responsibilities: [...draft.responsibilities, ""] })} className="min-h-11 w-fit rounded-md border border-dashed px-3 text-xs font-medium text-muted-foreground hover:bg-muted">+ Add responsibility</button>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="min-h-11 rounded-md border px-3 text-sm font-medium">Cancel</button>
        {onDelete && <button type="button" disabled={pending} onClick={onDelete} className="min-h-11 rounded-md border px-3 text-sm font-medium text-destructive">Delete seat</button>}
        <button type="button" disabled={pending || !draft.name.trim()} onClick={() => onSave({ ...draft, name: draft.name.trim(), responsibilities: draft.responsibilities.map((value) => value.trim()).filter(Boolean) })} className="min-h-11 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50">Save seat</button>
      </div>
    </div>
  );
}

export function OrgChartPage() {
  const wsId = useWorkspaceId();
  const chart = useQuery(orgChartOptions(wsId));
  const settings = useQuery(settingsOptions(wsId));
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const create = useCreateOrgChartSeat(wsId);
  const update = useUpdateOrgChartSeat(wsId);
  const remove = useDeleteOrgChartSeat(wsId);
  const [adding, setAdding] = useState(false);
  const [editingId, setEditingId] = useState<string>();
  const [insert, setInsert] = useState<InsertIntent>();

  const seats = chart.data?.seats ?? [];
  const memberList = members.data ?? [];
  const agentList = agents.data ?? [];
  const ownerOptions: SearchSelectOption[] = [
    ...memberList.map((member) => ({ value: `member:${member.id}`, label: member.name, group: "People" })),
    ...agentList.map((agent) => ({ value: `agent:${agent.id}`, label: agent.name, group: "Agents" })),
  ];

  // Seats store the owner by membership id (member.id, NOT user_id) and agent id,
  // matching the values in ownerOptions. Resolve the avatar from those same ids.
  const memberById = new Map(memberList.map((member) => [member.id, member]));
  const agentById = new Map(agentList.map((agent) => [agent.id, agent]));
  const resolveActor = (ownerType: "member" | "agent", ownerId: string): SeatActor | undefined => {
    if (ownerType === "member") {
      const member = memberById.get(ownerId);
      return member ? { name: member.name, initials: initialsOf(member.name), avatarUrl: member.avatar_url } : undefined;
    }
    const agent = agentById.get(ownerId);
    return agent ? { name: agent.name, initials: initialsOf(agent.name), avatarUrl: agent.avatar_url, isAgent: true } : undefined;
  };

  if (chart.isLoading) return <div className="mx-auto max-w-5xl p-6 text-sm text-muted-foreground">Loading org chart…</div>;
  if (chart.isError) return <div role="alert" className="mx-auto max-w-5xl p-6 text-sm text-destructive">Org chart could not be loaded.</div>;

  const editingSeat = seats.find((seat) => seat.id === editingId);
  const rolesLabel = settings.data?.terminology?.org_chart ?? "Roles";

  // The preset parent depends on the insert direction: a sibling ("beside") or a
  // new parent ("above") share the target's parent; a report ("below") nests under it.
  const insertTarget = insert ? seats.find((seat) => seat.id === insert.relativeToId) : undefined;
  const presetParentId = insert ? (insert.mode === "below" ? insert.relativeToId : insertTarget?.parent_id) : undefined;

  function startInsert(relativeToId: string, mode: InsertMode) {
    setEditingId(undefined);
    setInsert({ relativeToId, mode });
    setAdding(true);
  }

  function closeAdd() {
    setAdding(false);
    setInsert(undefined);
  }

  // Creating a seat "above" the target means the target reports into the new
  // seat, so re-parent it once the create returns its id.
  function handleCreate(input: OrgChartSeatInput) {
    create.mutate(input, {
      onSuccess: (created) => {
        if (insert?.mode === "above" && created && insertTarget) {
          update.mutate({ id: insertTarget.id, input: { ...inputFromSeat(insertTarget), parent_id: created.id } });
        }
        closeAdd();
      },
    });
  }

  return (
    <div className="mx-auto grid max-w-5xl gap-6 p-6">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{rolesLabel}</h1>
          <p className="mt-1 text-sm text-muted-foreground">Seats reporting into each other. Pick an owner by name; an empty seat stays visible as vacant.</p>
        </div>
        <button type="button" onClick={() => { setAdding(true); setEditingId(undefined); setInsert(undefined); }} className="min-h-11 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">+ Add seat</button>
      </div>

      {adding && <SeatEditor seats={seats} ownerOptions={ownerOptions} presetParentId={presetParentId} pending={create.isPending || update.isPending} onCancel={closeAdd} onSave={handleCreate} />}

      {editingSeat && <SeatEditor seat={editingSeat} seats={seats} ownerOptions={ownerOptions} pending={update.isPending || remove.isPending} onCancel={() => setEditingId(undefined)} onSave={(input) => update.mutate({ id: editingSeat.id, input }, { onSuccess: () => setEditingId(undefined) })} onDelete={() => remove.mutate(editingSeat.id, { onSuccess: () => setEditingId(undefined) })} />}

      {seats.length === 0 && !adding
        ? <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">No seats yet. Add the first seat to make ownership visible.</div>
        : <RolesChart seats={seats} onEdit={(id) => { setEditingId(id); setAdding(false); }} onInsert={startInsert} resolveActor={resolveActor} />}

      {(create.isError || update.isError || remove.isError) && <p role="alert" className="text-sm text-destructive">The org chart change could not be saved.</p>}
    </div>
  );
}
