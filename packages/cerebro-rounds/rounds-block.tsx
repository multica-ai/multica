"use client";

import { ChevronDown, ChevronRight, Pencil, Play, Plus, Search, Settings, Trash2, X } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle } from "@multica/ui/components/ui/drawer";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
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

export function RoundsBlock({ statuses, issueTitles, onStart, onSelectIssue, settings, renderIssue, dragHandle, defaultCollapsed, onRemove }: {
  statuses: RoundStatus[]; issueTitles: Record<string, string>; onStart: (id: string) => void; onSelectIssue: (id: string) => void;
  settings?: ReactNode;
  renderIssue?: (issueId: string) => ReactNode;
  dragHandle?: ReactNode;
  defaultCollapsed?: boolean;
  onRemove?: () => void;
}) {
  const visible = statuses.filter((s) => s.members.length > 0 || s.round.next_run_at || s.active_run);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  const [collapsed, setCollapsed] = useState(defaultCollapsed === true);
  const [query, setQuery] = useState("");
  const readyRoundId = visible.find((status) => status.active_run?.status === "ready")?.round.id ?? null;
  useEffect(() => {
    if (readyRoundId) setExpandedIds((prev) => (prev.has(readyRoundId) ? prev : new Set(prev).add(readyRoundId)));
  }, [readyRoundId]);
  const needle = query.trim().toLocaleLowerCase();
  const matchesByMember = (status: RoundStatus) => status.members.some((member) => (issueTitles[member.issue_id] ?? "").toLocaleLowerCase().includes(needle));
  useEffect(() => {
    if (!needle) return;
    const matches = visible.filter(matchesByMember).map((status) => status.round.id);
    if (matches.length === 0) return;
    setExpandedIds((prev) => new Set([...prev, ...matches]));
    // Only re-run when the search term changes; expanding on every render (e.g. new statuses) would fight manual collapses.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [needle]);
  const filtered = visible.filter((status) => !needle || status.round.name.toLocaleLowerCase().includes(needle) || matchesByMember(status));
  return <section className="overflow-hidden rounded-xl border border-border bg-card" aria-label="Rounds">
    <header className="flex items-center gap-2 border-b border-border px-3 py-2">
      {dragHandle}
      <button type="button" className="-ml-1 rounded p-0.5 text-muted-foreground hover:bg-muted" onClick={() => setCollapsed((value) => !value)} aria-label={`${collapsed ? "Expand" : "Collapse"} Rounds`}>
        {collapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
      </button>
      <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">Rounds</span>
      <div className="ml-auto">{settings}</div>
      {onRemove && <button type="button" className="rounded p-1 text-muted-foreground hover:bg-muted" onClick={onRemove} aria-label="Remove Rounds"><X className="size-3.5" /></button>}
    </header>
    {!collapsed && <>
      <div className="border-b border-border px-3 py-2"><div className="relative"><Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" /><Input type="search" aria-label="Search Rounds" placeholder="Search Rounds…" className="pl-8" value={query} onChange={(event) => setQuery(event.target.value)} /></div></div>
      <div className="divide-y divide-border/60">{filtered.map((s) => {
      const running = s.active_run?.status === "running";
      const ready = s.active_run?.status === "ready";
      const complete = s.members.length === 0 || (ready && (s.active_run?.responded_count ?? 0) >= (s.active_run?.total_count ?? s.members.length));
      const open = expandedIds.has(s.round.id);
      const schedule = nextRunLabel(s.round.next_run_at);
      return <div key={s.round.id}>
        <div className="flex items-center gap-2 px-3 py-2">
          <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${s.round.name}`} className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => setExpandedIds((prev) => {
            const next = new Set(prev);
            if (open) next.delete(s.round.id); else next.add(s.round.id);
            return next;
          })}>
            {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            <span className="truncate text-sm font-medium">{s.round.name}</span>
            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{s.round.mode === "live" ? "Live" : "Batch"}</span>
            {schedule && !running && !ready && <span className="hidden text-xs text-muted-foreground sm:inline">{schedule}</span>}
            <span className="ml-auto text-xs text-muted-foreground">{running || ready ? `${s.active_run?.responded_count ?? 0}/${s.active_run?.total_count ?? s.members.length} ready` : `${s.members.length} planned`}</span>
          </button>
          {!running && <Button size="sm" variant={ready ? "default" : "ghost"} disabled={complete} className={complete ? "bg-success text-success-foreground opacity-100" : undefined} onClick={() => onStart(s.round.id)} aria-label={`Run ${s.round.name}`}><Play className="size-3.5" /><span className="sr-only">Run</span></Button>}
        </div>
        {running && <div className="px-3 pb-2">
          <div role="progressbar" aria-label={`${s.round.name} progress`} aria-valuemin={0} aria-valuemax={s.active_run?.total_count ?? 0} aria-valuenow={s.active_run?.responded_count ?? 0} className="h-1.5 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${Math.min(100, ((s.active_run?.responded_count ?? 0) / Math.max(1, s.active_run?.total_count ?? 0)) * 100)}%` }} />
          </div>
          {(s.active_run?.nudged_count ?? 0) > 0 && <p className="mt-1 text-right text-[11px] text-muted-foreground">{s.active_run?.nudged_count} nudged</p>}
        </div>}
        {open && <div className="border-t border-border/50 divide-y divide-border/60">{s.members.map((m) =>
          renderIssue ? renderIssue(m.issue_id) : <button key={m.issue_id} type="button" aria-label={issueTitles[m.issue_id] ?? m.issue_id} onClick={() => onSelectIssue(m.issue_id)} className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-xs hover:bg-muted">
            <span className={`size-1.5 rounded-full ${running ? "bg-blue-500" : ready ? "bg-emerald-500" : "bg-muted-foreground"}`} />
            <span className="min-w-0 flex-1 truncate">{issueTitles[m.issue_id] ?? m.issue_id}</span>
            {m.held_trigger_count > 0 && <span className="text-muted-foreground">{m.held_trigger_count} held {m.held_trigger_count === 1 ? "response" : "responses"}</span>}
          </button>)}</div>}
      </div>;
    })}{filtered.length === 0 && <p className="px-3 py-3 text-xs text-muted-foreground">Nothing here right now.</p>}</div>
    </>}
  </section>;
}

function ResponsiveRoundPanel({ open, onOpenChange, title, showTrigger = false, children }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  showTrigger?: boolean;
  children: ReactNode;
}) {
  const isMobile = useIsMobile();
  // The panel is mounted inside the clickable inbox row (RoundPickerDialog in
  // CerebroInboxRowActions). The dialog/drawer renders in a portal, but React
  // synthetic clicks still bubble through the React tree to the row's onClick,
  // which navigates to the issue (FIR-3107). display:contents keeps the guard
  // out of the row's flex layout while its handler catches portal clicks.
  return <span className="contents" onClick={(event) => event.stopPropagation()}>
    {showTrigger && <Button type="button" variant="ghost" size="icon-sm" aria-label="Manage rounds" onClick={() => onOpenChange(true)}><Settings className="size-4" /></Button>}
    {isMobile ? <Drawer open={open} onOpenChange={onOpenChange}>
      {/* 85dvh, not 80vh: on phones vh is the large viewport (browser chrome
          collapsed), so an 80vh drawer overflows the visible area while the
          URL bar is showing (FIR-3107). dvh tracks what is actually visible. */}
      <DrawerContent className="data-[vaul-drawer-direction=bottom]:max-h-[85dvh]">
        <DrawerHeader><DrawerTitle>{title}</DrawerTitle></DrawerHeader>
        <div className="min-h-0 overflow-y-auto px-4 pb-6">{children}</div>
      </DrawerContent>
    </Drawer> : <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader><DialogTitle>{title}</DialogTitle></DialogHeader>
        {children}
      </DialogContent>
    </Dialog>}
  </span>;
}

export function RoundManager({ statuses, issueTitles, onCreate, onUpdate, onDelete, onRemoveMember }: {
  statuses: RoundStatus[]; issueTitles: Record<string, string>;
  onCreate: (input: RoundInput) => void; onUpdate: (id: string, input: RoundInput) => void;
  onDelete: (id: string) => void; onRemoveMember: (roundId: string, issueId: string) => void;
}) {
  const isMobile = useIsMobile();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<RoundStatus | null>(null);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [mode, setMode] = useState<"live" | "batch">("batch");
  const [schedulePreset, setSchedulePreset] = useState<"manual" | "daily" | "weekdays" | "weekly">("manual");
  const [scheduleTime, setScheduleTime] = useState("09:00");
  const [timezone, setTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
  const beginCreate = () => { setEditing(null); setName(""); setMode("batch"); setSchedulePreset("manual"); setScheduleTime("09:00"); setCreating(true); };
  const presetFor = (cron: string | null | undefined) => !cron ? "manual" : cron.endsWith("* * 1-5") ? "weekdays" : cron.endsWith("* * 1") ? "weekly" : "daily";
  const timeFor = (cron: string | null | undefined) => { const parts = cron?.split(" "); return parts && parts.length >= 2 ? `${parts[1]!.padStart(2, "0")}:${parts[0]!.padStart(2, "0")}` : "09:00"; };
  const beginEdit = (s: RoundStatus) => { setEditing(s); setName(s.round.name); setMode(s.round.mode); setSchedulePreset(presetFor(s.round.schedule_cron)); setScheduleTime(timeFor(s.round.schedule_cron)); setTimezone(s.round.timezone ?? "UTC"); setCreating(true); };
  const save = () => {
    const [hour = "9", minute = "0"] = scheduleTime.split(":");
    const suffix = schedulePreset === "weekdays" ? "* * 1-5" : schedulePreset === "weekly" ? "* * 1" : "* * *";
    const schedule = schedulePreset === "manual" ? null : `${Number(minute)} ${Number(hour)} ${suffix}`;
    const input = { name: name.trim(), mode, schedule_cron: schedule, timezone: timezone.trim() || "UTC" };
    if (!input.name) return;
    if (editing) onUpdate(editing.round.id, input); else onCreate(input);
    setCreating(false);
  };
  return <ResponsiveRoundPanel open={open} onOpenChange={setOpen} title="Manage rounds" showTrigger>
      {creating ? <div className="grid gap-4">
        {/* No autoFocus on mobile: it opens the keyboard over the lower half
            of the form before the user has seen it (FIR-3107). */}
        <div className="grid gap-1.5"><Label htmlFor="round-name">Round name</Label><Input id="round-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus={!isMobile} /></div>
        <fieldset className="grid gap-2"><legend className="text-sm font-medium">Type</legend><div className="grid grid-cols-2 gap-2">{(["live", "batch"] as const).map((value) => <Button key={value} type="button" variant={mode === value ? "default" : "outline"} role="radio" aria-checked={mode === value} aria-label={value === "live" ? "Live" : "Batch"} onClick={() => setMode(value)}>{value === "live" ? "Live" : "Batch"}</Button>)}</div><p className="text-xs text-muted-foreground">{mode === "live" ? "Agents work immediately; replies wait in this Round." : "Work waits until you press Run."}</p></fieldset>
        <fieldset className="grid gap-2"><legend className="text-sm font-medium">Schedule</legend><div className="grid grid-cols-2 gap-2">{(["manual", "daily", "weekdays", "weekly"] as const).map((value) => <Button key={value} type="button" variant={schedulePreset === value ? "default" : "outline"} onClick={() => setSchedulePreset(value)}>{value[0]!.toUpperCase() + value.slice(1)}</Button>)}</div></fieldset>
        {schedulePreset !== "manual" && <div className="grid gap-1.5"><Label htmlFor="round-time">Time</Label><Input id="round-time" type="time" value={scheduleTime} onChange={(event) => setScheduleTime(event.target.value)} /></div>}
        <div className="grid gap-1.5"><Label htmlFor="round-timezone">Timezone</Label><Input id="round-timezone" value={timezone} onChange={(e) => setTimezone(e.target.value)} /></div>
        {/* Sticky: Save/Cancel stay on screen while the form above scrolls or
            the mobile keyboard pushes content out of view (FIR-3107). The
            opaque background keeps scrolled-under fields from showing through. */}
        <DialogFooter className="sticky bottom-0 bg-popover"><Button variant="ghost" onClick={() => setCreating(false)}>Cancel</Button><Button onClick={save} disabled={!name.trim()} aria-label="Save round">Save</Button></DialogFooter>
      </div> : <div className="grid gap-3">
        <Button variant="outline" onClick={beginCreate} aria-label="Create round"><Plus className="size-4" />Create round</Button>
        {statuses.map((s) => <div key={s.round.id} className="rounded-lg border p-3">
          <div className="flex items-center gap-2"><div className="min-w-0 flex-1"><p className="truncate font-medium">{s.round.name}</p><p className="text-xs text-muted-foreground">{s.round.mode === "live" ? "Live" : "Batch"} · {presetFor(s.round.schedule_cron)[0]!.toUpperCase() + presetFor(s.round.schedule_cron).slice(1)}</p></div>
            <Button variant="ghost" size="icon-sm" aria-label={`Edit ${s.round.name}`} onClick={() => beginEdit(s)}><Pencil className="size-3.5" /></Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Delete ${s.round.name}`} onClick={() => onDelete(s.round.id)}><Trash2 className="size-3.5" /></Button>
          </div>
          {s.members.map((m) => <div key={m.issue_id} className="mt-2 flex items-center gap-2 text-xs"><span className="min-w-0 flex-1 truncate">{issueTitles[m.issue_id] ?? m.issue_id}</span><Button variant="ghost" size="icon-sm" aria-label={`Remove ${issueTitles[m.issue_id] ?? m.issue_id}`} onClick={() => onRemoveMember(s.round.id, m.issue_id)}><X className="size-3.5" /></Button></div>)}
        </div>)}
      </div>}
  </ResponsiveRoundPanel>;
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

export function RoundPickerDialog({ issueId, open, onOpenChange }: { issueId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const wsId = useWorkspaceId();
  const { data: statuses = [] } = useRoundStatuses(wsId);
  const add = useAddIssueToRound(wsId);
  const available = statuses.filter((status) => !status.members.some((member) => member.issue_id === issueId));
  return <ResponsiveRoundPanel open={open} onOpenChange={onOpenChange} title="Add to Round">
      <div className="grid gap-2">
        {available.map((status) => <Button key={status.round.id} variant="outline" className="h-auto justify-start px-3 py-2 text-left" onClick={() => {
          add.mutate({ roundId: status.round.id, issueId });
          onOpenChange(false);
        }}><span className="min-w-0 flex-1"><span className="block truncate font-medium">{status.round.name}</span><span className="block text-xs text-muted-foreground">{status.round.mode === "live" ? "Live" : "Batch"}</span></span></Button>)}
        {available.length === 0 && <p className="py-4 text-center text-sm text-muted-foreground">Create a Round from Inbox first.</p>}
      </div>
  </ResponsiveRoundPanel>;
}

export function ConnectedRoundManager({ statuses, issueTitles }: { statuses: RoundStatus[]; issueTitles: Record<string, string> }) {
  const wsId = useWorkspaceId();
  const create = useCreateRound(wsId); const update = useUpdateRound(wsId); const remove = useRemoveIssueFromRound(wsId); const del = useDeleteRound(wsId);
  return <RoundManager statuses={statuses} issueTitles={issueTitles} onCreate={(input) => create.mutate(input)} onUpdate={(id, input) => update.mutate({ id, input })} onDelete={(id) => del.mutate(id)} onRemoveMember={(roundId, issueId) => remove.mutate({ roundId, issueId })} />;
}
