"use client";

import { ChevronDown, ChevronRight, Pencil, Play, Plus, Settings, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAddIssueToRound, useCreateRound, useDeleteRound, useRemoveIssueFromRound, useRoundStatuses, useUpdateRound } from "./queries";
import { roundMembershipLabel, type RoundStatus } from "./schemas";
import type { RoundInput } from "./api";

function nextRunLabel(value: string | null | undefined): string | null {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return `Next ${new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" }).format(date)}`;
}

export function RoundsBlock({ statuses, issueTitles, onStart, onSelectIssue }: {
  statuses: RoundStatus[]; issueTitles: Record<string, string>; onStart: (id: string) => void; onSelectIssue: (id: string) => void;
}) {
  const visible = statuses.filter((s) => s.members.length > 0 || s.round.next_run_at || s.active_run);
  const [expanded, setExpanded] = useState<string | null>(null);
  const readyRoundId = visible.find((status) => status.active_run?.status === "ready")?.round.id ?? null;
  useEffect(() => {
    if (readyRoundId) setExpanded(readyRoundId);
  }, [readyRoundId]);
  if (visible.length === 0) return null;
  return <section className="overflow-hidden rounded-lg border border-border bg-card" aria-label="Rounds">
    {visible.map((s) => {
      const running = s.active_run?.status === "running";
      const ready = s.active_run?.status === "ready";
      const open = expanded === s.round.id;
      const schedule = nextRunLabel(s.round.next_run_at);
      return <div key={s.round.id} className="border-b border-border/60 last:border-0">
        <div className="flex items-center gap-2 px-3 py-2">
          <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${s.round.name}`} className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => setExpanded(open ? null : s.round.id)}>
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            <span className="truncate text-sm font-medium">{s.round.name}</span>
            {schedule && !running && !ready && <span className="hidden text-xs text-muted-foreground sm:inline">{schedule}</span>}
            <span className="ml-auto text-xs text-muted-foreground">{running || ready ? `${s.active_run?.responded_count ?? 0}/${s.active_run?.total_count ?? s.members.length} ready` : `${s.members.length} planned`}</span>
          </button>
          {!running && <Button size="sm" variant={ready ? "default" : "ghost"} onClick={() => onStart(s.round.id)} aria-label={`${ready ? "Start next" : "Start"} ${s.round.name}`}><Play className="size-3.5" /></Button>}
        </div>
        {running && <div className="px-3 pb-2">
          <div role="progressbar" aria-label={`${s.round.name} progress`} aria-valuemin={0} aria-valuemax={s.active_run?.total_count ?? 0} aria-valuenow={s.active_run?.responded_count ?? 0} className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${Math.min(100, ((s.active_run?.responded_count ?? 0) / Math.max(1, s.active_run?.total_count ?? 0)) * 100)}%` }} />
          </div>
          {(s.active_run?.nudged_count ?? 0) > 0 && <p className="mt-1 text-right text-[11px] text-muted-foreground">{s.active_run?.nudged_count} nudged</p>}
        </div>}
        {open && <div className="border-t border-border/50 px-3 py-1.5">{s.members.map((m) => <button key={m.issue_id} type="button" aria-label={issueTitles[m.issue_id] ?? m.issue_id} onClick={() => onSelectIssue(m.issue_id)} className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-muted">
          <span className={`size-1.5 rounded-full ${running ? "bg-blue-500" : ready ? "bg-emerald-500" : "bg-muted-foreground"}`} />
          <span className="min-w-0 flex-1 truncate">{issueTitles[m.issue_id] ?? m.issue_id}</span>
          {m.held_trigger_count > 0 && <span className="text-muted-foreground">{m.held_trigger_count} held {m.held_trigger_count === 1 ? "response" : "responses"}</span>}
        </button>)}</div>}
      </div>;
    })}
  </section>;
}

export function RoundManager({ statuses, issueTitles, onCreate, onUpdate, onDelete, onRemoveMember }: {
  statuses: RoundStatus[]; issueTitles: Record<string, string>;
  onCreate: (input: RoundInput) => void; onUpdate: (id: string, input: RoundInput) => void;
  onDelete: (id: string) => void; onRemoveMember: (roundId: string, issueId: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<RoundStatus | null>(null);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [schedule, setSchedule] = useState("");
  const [timezone, setTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
  const beginCreate = () => { setEditing(null); setName(""); setSchedule(""); setCreating(true); };
  const beginEdit = (s: RoundStatus) => { setEditing(s); setName(s.round.name); setSchedule(s.round.schedule_cron ?? ""); setTimezone(s.round.timezone ?? "UTC"); setCreating(true); };
  const save = () => {
    const input = { name: name.trim(), schedule_cron: schedule.trim() || null, timezone: timezone.trim() || "UTC" };
    if (!input.name) return;
    if (editing) onUpdate(editing.round.id, input); else onCreate(input);
    setCreating(false);
  };
  return <Dialog open={open} onOpenChange={setOpen}>
    <DialogTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Manage rounds" />}><Settings className="size-4" /></DialogTrigger>
    <DialogContent className="sm:max-w-lg">
      <DialogHeader><DialogTitle>Manage rounds</DialogTitle></DialogHeader>
      {creating ? <div className="grid gap-4">
        <div className="grid gap-1.5"><Label htmlFor="round-name">Round name</Label><Input id="round-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus /></div>
        <div className="grid gap-1.5"><Label htmlFor="round-schedule">Schedule (cron)</Label><Input id="round-schedule" value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="0 9 * * 1-5" /></div>
        <div className="grid gap-1.5"><Label htmlFor="round-timezone">Timezone</Label><Input id="round-timezone" value={timezone} onChange={(e) => setTimezone(e.target.value)} /></div>
        <DialogFooter><Button variant="ghost" onClick={() => setCreating(false)}>Cancel</Button><Button onClick={save} disabled={!name.trim()} aria-label="Save round">Save</Button></DialogFooter>
      </div> : <div className="grid gap-3">
        <Button variant="outline" onClick={beginCreate} aria-label="Create round"><Plus className="size-4" />Create round</Button>
        {statuses.map((s) => <div key={s.round.id} className="rounded-lg border p-3">
          <div className="flex items-center gap-2"><div className="min-w-0 flex-1"><p className="truncate font-medium">{s.round.name}</p><p className="text-xs text-muted-foreground">{s.round.schedule_cron || "Manual"} · {s.round.timezone || "UTC"}</p></div>
            <Button variant="ghost" size="icon-sm" aria-label={`Edit ${s.round.name}`} onClick={() => beginEdit(s)}><Pencil className="size-3.5" /></Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Delete ${s.round.name}`} onClick={() => onDelete(s.round.id)}><Trash2 className="size-3.5" /></Button>
          </div>
          {s.members.map((m) => <div key={m.issue_id} className="mt-2 flex items-center gap-2 text-xs"><span className="min-w-0 flex-1 truncate">{issueTitles[m.issue_id] ?? m.issue_id}</span><Button variant="ghost" size="icon-sm" aria-label={`Remove ${issueTitles[m.issue_id] ?? m.issue_id}`} onClick={() => onRemoveMember(s.round.id, m.issue_id)}><X className="size-3.5" /></Button></div>)}
        </div>)}
      </div>}
    </DialogContent>
  </Dialog>;
}

export function AddToRoundAction({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const { data: statuses = [] } = useRoundStatuses(wsId);
  const add = useAddIssueToRound(wsId);
  const available = statuses.filter((s) => !s.members.some((m) => m.issue_id === issueId));
  const membership = roundMembershipLabel(statuses, issueId);
  if (available.length === 0 && !membership) return null;
  return <div className="flex flex-wrap items-center gap-2">{membership && <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground" aria-label="Round status">{membership}</span>}{available.length > 0 && <DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>Add to round</DropdownMenuTrigger><DropdownMenuContent align="start">{available.map((s) => <DropdownMenuItem key={s.round.id} onClick={() => add.mutate({ roundId: s.round.id, issueId })}>{s.round.name}</DropdownMenuItem>)}</DropdownMenuContent></DropdownMenu>}</div>;
}

export function ConnectedRoundManager({ statuses, issueTitles }: { statuses: RoundStatus[]; issueTitles: Record<string, string> }) {
  const wsId = useWorkspaceId();
  const create = useCreateRound(wsId); const update = useUpdateRound(wsId); const remove = useRemoveIssueFromRound(wsId); const del = useDeleteRound(wsId);
  return <RoundManager statuses={statuses} issueTitles={issueTitles} onCreate={(input) => create.mutate(input)} onUpdate={(id, input) => update.mutate({ id, input })} onDelete={(id) => del.mutate(id)} onRemoveMember={(roundId, issueId) => remove.mutate({ roundId, issueId })} />;
}
