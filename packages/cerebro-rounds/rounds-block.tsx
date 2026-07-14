"use client";

import { ChevronDown, ChevronRight, Pencil, Play, Plus, Search, Settings, Trash2, X } from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";
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

type RoundView = "ready" | "handled" | "all";

export function RoundsBlock({
  statuses,
  issueTitles,
  messageIssueIds,
  issueRunStates = new Map(),
  onStart,
  onSelectIssue,
  settings,
  renderIssue,
  dragHandle,
  defaultCollapsed,
  onRemove,
}: {
  statuses: RoundStatus[];
  issueTitles: Record<string, string>;
  messageIssueIds?: readonly string[];
  issueRunStates?: ReadonlyMap<string, unknown>;
  onStart: (id: string) => void;
  onSelectIssue: (id: string) => void;
  settings?: ReactNode;
  renderIssue?: (issueId: string) => ReactNode;
  dragHandle?: ReactNode;
  defaultCollapsed?: boolean;
  onRemove?: () => void;
}) {
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  const [collapsed, setCollapsed] = useState(defaultCollapsed === true);
  const [query, setQuery] = useState("");
  const [views, setViews] = useState<Record<string, RoundView>>({});
  const needle = query.trim().toLocaleLowerCase();

  const renderMember = (issueId: string) => {
    if (renderIssue) return <Fragment key={issueId}>{renderIssue(issueId)}</Fragment>;
    return <button key={issueId} type="button" aria-label={issueTitles[issueId] ?? issueId} onClick={() => onSelectIssue(issueId)} className="flex w-full min-w-0 items-center px-4 py-2.5 text-left text-xs hover:bg-muted">
        <span className="min-w-0 flex-1 truncate">{issueTitles[issueId] ?? issueId}</span>
      </button>;
  };

  const renderRound = (s: RoundStatus) => {
    const open = expandedIds.has(s.round.id);
    const view = views[s.round.id] ?? "ready";
    const allIds = s.members.map((member) => member.issue_id);
    const memberIds = new Set(allIds);
    const orderedIds = (messageIssueIds ?? allIds).filter((issueId) => memberIds.has(issueId));
    const readySet = new Set(
      (s.active_cycle?.items ?? [])
        .filter((item) => item.handled_at == null && !issueRunStates.has(item.issue_id))
        .map((item) => item.issue_id),
    );
    const handledSet = new Set(
      (s.active_cycle?.items ?? [])
        .filter((item) => item.handled_at != null)
        .map((item) => item.issue_id),
    );
    const readyIds = orderedIds.filter((issueId) => readySet.has(issueId));
    const handledIds = orderedIds.filter((issueId) => handledSet.has(issueId));
    const ids = (view === "ready" ? readyIds : view === "handled" ? handledIds : orderedIds)
      .filter((issueId) => !needle || (issueTitles[issueId] ?? issueId).toLocaleLowerCase().includes(needle));
    const summary = s.active_cycle ? `${readyIds.length} ready` : "Ready to start";
    return <div key={s.round.id}>
      <div className="flex items-center gap-2 px-3 py-2">
        <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${s.round.name}`} className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => setExpandedIds((prev) => {
          const next = new Set(prev);
          if (open) next.delete(s.round.id); else next.add(s.round.id);
          return next;
        })}>
          {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          <span className="truncate text-sm font-medium">{s.round.name}</span>
          <span className="ml-auto text-xs text-muted-foreground">{summary}</span>
        </button>
        <Button size="sm" variant="ghost" onClick={() => {
          setViews((current) => ({ ...current, [s.round.id]: "ready" }));
          setExpandedIds((current) => new Set([...current, s.round.id]));
          onStart(s.round.id);
        }} aria-label={`Play ${s.round.name}`}><Play className="size-3.5" /><span className="sr-only">Play</span></Button>
      </div>
      {open && <>
        <div className="flex gap-1 border-t border-border/50 px-3 py-2" role="group" aria-label={`${s.round.name} view`}>
          {(["ready", "handled", "all"] as const).map((value) => <Button key={value} size="sm" variant={view === value ? "secondary" : "ghost"} aria-pressed={view === value} onClick={() => setViews((current) => ({ ...current, [s.round.id]: value }))}>
            {value === "ready" ? "Ready" : value === "handled" ? "Handled this round" : "All messages"}
          </Button>)}
        </div>
        <div className="divide-y divide-border/60 border-t border-border/50">
          {ids.map(renderMember)}
          {ids.length === 0 && <p className="px-3 py-3 text-xs text-muted-foreground">Nothing here right now.</p>}
        </div>
      </>}
    </div>;
  };
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
      <div className="divide-y divide-border/60">
        {statuses.map(renderRound)}
        {statuses.length === 0 && <p className="px-3 py-3 text-xs text-muted-foreground">Nothing here right now.</p>}
      </div>
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
  const beginCreate = () => { setEditing(null); setName(""); setCreating(true); };
  const beginEdit = (s: RoundStatus) => { setEditing(s); setName(s.round.name); setCreating(true); };
  const save = () => {
    const input = { name: name.trim() };
    if (!input.name) return;
    if (editing) onUpdate(editing.round.id, input); else onCreate(input);
    setCreating(false);
  };
  return <ResponsiveRoundPanel open={open} onOpenChange={setOpen} title="Manage rounds" showTrigger>
      {creating ? <div className="grid gap-4">
        {/* No autoFocus on mobile: it opens the keyboard over the lower half
            of the form before the user has seen it (FIR-3107). */}
        <div className="grid gap-1.5"><Label htmlFor="round-name">Round name</Label><Input id="round-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus={!isMobile} /></div>
        {/* Sticky: Save/Cancel stay on screen while the form above scrolls or
            the mobile keyboard pushes content out of view (FIR-3107). The
            opaque background keeps scrolled-under fields from showing through. */}
        <DialogFooter className="sticky bottom-0 bg-popover"><Button variant="ghost" onClick={() => setCreating(false)}>Cancel</Button><Button onClick={save} disabled={!name.trim()} aria-label="Save round">Save</Button></DialogFooter>
      </div> : <div className="grid gap-3">
        <Button variant="outline" onClick={beginCreate} aria-label="Create round"><Plus className="size-4" />Create round</Button>
        {statuses.map((s) => <div key={s.round.id} className="rounded-lg border p-3">
          <div className="flex items-center gap-2"><div className="min-w-0 flex-1"><p className="truncate font-medium">{s.round.name}</p></div>
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
  const remove = useRemoveIssueFromRound(wsId);
  const current = statuses.find((s) => s.members.some((m) => m.issue_id === issueId));
  const available = statuses.filter((s) => s.round.id !== current?.round.id);
  const membership = roundMembershipLabel(statuses, issueId);
  if (available.length === 0 && !current) return null;
  // A member issue can move to another round or leave rounds entirely
  // (FIR-3114 review, point 5). Membership is unique per issue, so a move
  // must remove before it adds.
  const moveTo = async (roundId: string) => {
    if (current) await remove.mutateAsync({ roundId: current.round.id, issueId });
    add.mutate({ roundId, issueId });
  };
  return <div className="flex flex-wrap items-center gap-2">{membership && <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground" aria-label="Round status">{membership}</span>}<DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>{current ? "Move to round" : "Add to round"}</DropdownMenuTrigger><DropdownMenuContent align="start">
    {available.map((s) => <DropdownMenuItem key={s.round.id} onClick={() => void moveTo(s.round.id)}>{s.round.name}</DropdownMenuItem>)}
    {current && <DropdownMenuItem onClick={() => remove.mutate({ roundId: current.round.id, issueId })}>Remove from {current.round.name}</DropdownMenuItem>}
  </DropdownMenuContent></DropdownMenu></div>;
}

export function RoundPickerDialog({ issueId, open, onOpenChange }: { issueId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const wsId = useWorkspaceId();
  const { data: statuses = [] } = useRoundStatuses(wsId);
  const add = useAddIssueToRound(wsId);
  const remove = useRemoveIssueFromRound(wsId);
  const current = statuses.find((status) => status.members.some((member) => member.issue_id === issueId));
  const available = statuses.filter((status) => status.round.id !== current?.round.id);
  // A member issue can move to another round or leave rounds entirely
  // (FIR-3114 review, point 5). Membership is unique per issue, so a move
  // must remove before it adds.
  const moveTo = async (roundId: string) => {
    if (current) await remove.mutateAsync({ roundId: current.round.id, issueId });
    add.mutate({ roundId, issueId });
  };
  return <ResponsiveRoundPanel open={open} onOpenChange={onOpenChange} title={current ? "Move to Round" : "Add to Round"}>
      <div className="grid gap-2">
        {available.map((status) => <Button key={status.round.id} variant="outline" className="h-auto justify-start px-3 py-2 text-left" onClick={() => {
          void moveTo(status.round.id);
          onOpenChange(false);
        }}><span className="min-w-0 flex-1"><span className="block truncate font-medium">{status.round.name}</span></span></Button>)}
        {current && <Button variant="outline" className="h-auto justify-start px-3 py-2 text-left" onClick={() => {
          remove.mutate({ roundId: current.round.id, issueId });
          onOpenChange(false);
        }} aria-label={`Remove from ${current.round.name}`}><span className="min-w-0 flex-1"><span className="block truncate font-medium">Remove from {current.round.name}</span><span className="block text-xs text-muted-foreground">Back to the normal inbox</span></span></Button>}
        {available.length === 0 && !current && <p className="py-4 text-center text-sm text-muted-foreground">Create a Round from Inbox first.</p>}
      </div>
  </ResponsiveRoundPanel>;
}

export function ConnectedRoundManager({ statuses, issueTitles }: { statuses: RoundStatus[]; issueTitles: Record<string, string> }) {
  const wsId = useWorkspaceId();
  const create = useCreateRound(wsId); const update = useUpdateRound(wsId); const remove = useRemoveIssueFromRound(wsId); const del = useDeleteRound(wsId);
  return <RoundManager statuses={statuses} issueTitles={issueTitles} onCreate={(input) => create.mutate(input)} onUpdate={(id, input) => update.mutate({ id, input })} onDelete={(id) => del.mutate(id)} onRemoveMember={(roundId, issueId) => remove.mutate({ roundId, issueId })} />;
}
