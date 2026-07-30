"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { orgChartOptions, settingsOptions, useCreateOrgChartSeat, useDeleteOrgChartSeat, useUpdateOrgChartSeat } from "../core/queries";
import type { OrgChartSeat, OrgChartSeatInput } from "../core/types";
import { OsPageShell } from "./os-page-shell";
import { SearchSelect, type SearchSelectOption } from "./search-select";
import { RolesChart, type InsertMode, type SeatActor } from "./roles-chart";
import { PeopleRolesTable, type RoleHolder } from "./people-roles-table";

function inputFromSeat(seat: OrgChartSeat): OrgChartSeatInput {
  return { parent_id: seat.parent_id, name: seat.name, responsibilities: seat.responsibilities, owners: seat.owners.map((owner) => ({ type: owner.type, id: owner.id })), position: seat.position };
}

function initialsOf(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase() ?? "").join("") || "?";
}

// Where a new seat lands relative to the clicked one: "beside" is a sibling,
// "below" a direct report, and "above" a new parent the clicked seat moves under.
interface InsertIntent { relativeToId: string; mode: InsertMode }

// The owner picker speaks names, never IDs: one searchable field lists the
// workspace's people and agents, and "Vacant" is simply an empty selection.
// A seat may hold several of them at once (FIR-3589 item 9).
const ownerValues = (owners: { type: "member" | "agent"; id: string }[]) => owners.map((owner) => `${owner.type}:${owner.id}`);

function ownersFromValues(values: string[]): { type: "member" | "agent"; id: string }[] {
  return values.flatMap((value) => {
    const [ownerType, ownerId] = value.split(":");
    if (!ownerId || (ownerType !== "member" && ownerType !== "agent")) return [];
    return [{ type: ownerType, id: ownerId }];
  });
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
  const [draft, setDraft] = useState<OrgChartSeatInput>(seat ? inputFromSeat(seat) : { name: "", responsibilities: [], owners: [], position: seats.length, parent_id: presetParentId });
  const parentOptions = seats.filter((candidate) => candidate.id !== seat?.id).map((candidate) => ({ value: candidate.id, label: candidate.name }));

  function setResponsibility(index: number, value: string) {
    setDraft({ ...draft, responsibilities: draft.responsibilities.map((item, current) => current === index ? value : item) });
  }

  return (
    <div className="grid gap-3">
      <div className="grid gap-3 md:grid-cols-2">
        <label className="grid gap-1 text-sm font-medium">Seat name
          <input aria-label="Seat name" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="e.g. Operations" className="min-h-11 rounded-md border bg-background px-3" />
        </label>
        <label className="grid gap-1 text-sm font-medium">Reports to
          <SearchSelect label="Reports to" compact options={parentOptions} value={draft.parent_id ?? ""} onChange={(value) => setDraft({ ...draft, parent_id: value || undefined })} clearLabel="No parent (top of chart)" placeholder="Select a seat" />
        </label>
      </div>

      <div className="grid gap-1 text-sm font-medium">Name(s)
        <SearchSelect
          label="Name(s)"
          fieldLabel="Name(s)"
          compact
          multiple
          options={ownerOptions}
          values={ownerValues(draft.owners)}
          onValuesChange={(values) => setDraft({ ...draft, owners: ownersFromValues(values) })}
          placeholder="Vacant — search people and agents"
        />
        {draft.owners.length > 0 && (
          <ul aria-label="Selected names" className="flex flex-wrap gap-1.5 pt-1">
            {draft.owners.map((owner) => {
              const value = `${owner.type}:${owner.id}`;
              const name = ownerOptions.find((option) => option.value === value)?.label ?? owner.id;
              return (
                <li key={value}>
                  <button type="button" aria-label={`Remove ${name}`} onClick={() => setDraft({ ...draft, owners: draft.owners.filter((candidate) => `${candidate.type}:${candidate.id}` !== value) })} className="flex min-h-8 max-w-full items-center gap-1 rounded-full border bg-muted/40 px-2.5 text-xs font-normal">
                    <span className="truncate">{name}</span>
                    <span aria-hidden className="text-muted-foreground">×</span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

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

// One presentation, two shapes: a centred modal from the `sm` breakpoint up,
// and a sheet anchored to the bottom edge of the screen on a phone.
function SeatEditorDialog({ title, open, onOpenChange, children }: { title: string; open: boolean; onOpenChange: (open: boolean) => void; children: React.ReactNode }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-auto bottom-0 left-0 max-h-[85dvh] max-w-none translate-x-0 translate-y-0 rounded-b-none sm:top-1/2 sm:bottom-auto sm:left-1/2 sm:max-w-2xl sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-xl">
        <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
        {children}
      </DialogContent>
    </Dialog>
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

  // The People tab reads the same seats from the other direction: every person
  // and agent in the workspace, with the roles they currently hold.
  const rolesByOwner = new Map<string, string[]>();
  for (const seat of seats) {
    for (const owner of seat.owners) {
      const key = `${owner.type}:${owner.id}`;
      rolesByOwner.set(key, [...(rolesByOwner.get(key) ?? []), seat.name]);
    }
  }
  const holders: RoleHolder[] = [
    ...memberList.map((member) => ({ key: `member:${member.id}`, name: member.name, type: "member" as const, initials: initialsOf(member.name), avatarUrl: member.avatar_url, roles: rolesByOwner.get(`member:${member.id}`) ?? [] })),
    ...agentList.map((agent) => ({ key: `agent:${agent.id}`, name: agent.name, type: "agent" as const, initials: initialsOf(agent.name), avatarUrl: agent.avatar_url, roles: rolesByOwner.get(`agent:${agent.id}`) ?? [] })),
  ].sort((a, b) => b.roles.length - a.roles.length || a.name.localeCompare(b.name));
  const vacantRoles = seats.filter((seat) => seat.owners.length === 0).map((seat) => seat.name);

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
    <OsPageShell
      title={rolesLabel}
      subtitle="Seats reporting into each other — an empty seat stays visible as vacant"
      headerActions={<button type="button" onClick={() => { setAdding(true); setEditingId(undefined); setInsert(undefined); }} className="h-8 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground">+ Add role</button>}
    >
      <div className="grid w-full gap-4 p-4 md:p-6">
      {chart.isLoading && <p className="text-sm text-muted-foreground">Loading org chart…</p>}
      {chart.isError && <p role="alert" className="text-sm text-destructive">Org chart could not be loaded.</p>}

      <SeatEditorDialog title="Add role" open={adding} onOpenChange={(open) => { if (!open) closeAdd(); }}>
        <SeatEditor seats={seats} ownerOptions={ownerOptions} presetParentId={presetParentId} pending={create.isPending || update.isPending} onCancel={closeAdd} onSave={handleCreate} />
      </SeatEditorDialog>

      <SeatEditorDialog title={editingSeat ? `Edit ${editingSeat.name}` : "Edit role"} open={Boolean(editingSeat)} onOpenChange={(open) => { if (!open) setEditingId(undefined); }}>
        {editingSeat && <SeatEditor seat={editingSeat} seats={seats} ownerOptions={ownerOptions} pending={update.isPending || remove.isPending} onCancel={() => setEditingId(undefined)} onSave={(input) => update.mutate({ id: editingSeat.id, input }, { onSuccess: () => setEditingId(undefined) })} onDelete={() => remove.mutate(editingSeat.id, { onSuccess: () => setEditingId(undefined) })} />}
      </SeatEditorDialog>

      <Tabs defaultValue="chart" className="gap-4">
        <TabsList>
          <TabsTrigger value="chart">Chart</TabsTrigger>
          <TabsTrigger value="people">People</TabsTrigger>
        </TabsList>
        <TabsContent value="chart">
          {seats.length === 0
            ? <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">No seats yet. Add the first seat to make ownership visible.</div>
            : <RolesChart seats={seats} onEdit={(id) => { setEditingId(id); setAdding(false); }} onInsert={startInsert} resolveActor={resolveActor} />}
        </TabsContent>
        <TabsContent value="people">
          <PeopleRolesTable holders={holders} vacantRoles={vacantRoles} />
        </TabsContent>
      </Tabs>

      {(create.isError || update.isError || remove.isError) && <p role="alert" className="text-sm text-destructive">The org chart change could not be saved.</p>}
      </div>
    </OsPageShell>
  );
}
