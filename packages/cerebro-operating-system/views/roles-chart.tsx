"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, Plus, UserRoundX } from "lucide-react";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";

import type { OrgChartSeat } from "../core/types";

// A resolved owner: name + initials for the avatar fallback, plus the picture
// URL when the member/agent has one. Agents render the robot glyph on fallback.
export interface SeatActor {
  name: string;
  initials: string;
  avatarUrl?: string | null;
  isAgent?: boolean;
}

export type InsertMode = "above" | "beside" | "below";

// One fixed colour code per ownership state so the chart reads at a glance:
// agents are violet, people are sky, and an empty seat stays amber. The class
// shape matches the workspace's categorical-status convention (see HealthBadge).
const ACCENT = {
  agent: { bar: "border-l-violet-500", dot: "bg-violet-500", label: "text-violet-700 dark:text-violet-300" },
  member: { bar: "border-l-sky-500", dot: "bg-sky-500", label: "text-sky-700 dark:text-sky-300" },
  vacant: { bar: "border-l-amber-500", dot: "bg-amber-500", label: "text-amber-700 dark:text-amber-300" },
} as const;

function seatState(seat: OrgChartSeat): keyof typeof ACCENT {
  if (!seat.owner_name) return "vacant";
  return seat.owner_type === "agent" ? "agent" : "member";
}

function initialsOf(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase() ?? "").join("") || "?";
}

// Every card is the same size so a level reads as one row: the card never grows
// with its text, and anything past the cap collapses into a "+N more" line.
const CARD = "h-60 w-64 shrink-0";
const MAX_ACCOUNTABILITIES = 4;

function seatLabel(seat: OrgChartSeat): string {
  if (!seat.owner_name) return `${seat.name}, Vacant`;
  return `${seat.name}, ${seat.owner_type === "agent" ? "Agent" : "Owner"} ${seat.owner_name}`;
}

// The small round + that sits on a connector line, one per direction. Kept
// visually tiny per the org-chart design, with a padded hit area for touch.
function AddRoleButton({ seat, mode, onInsert, className }: {
  seat: OrgChartSeat;
  mode: InsertMode;
  onInsert: (relativeToId: string, mode: InsertMode) => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-label={`Add role ${mode} ${seat.name}`}
      onClick={() => onInsert(seat.id, mode)}
      className={`relative z-20 grid size-6 place-items-center rounded-full border bg-background text-muted-foreground shadow-sm before:absolute before:-inset-2 before:content-[''] hover:bg-muted hover:text-foreground ${className ?? ""}`}
    >
      <Plus aria-hidden className="size-3.5" />
    </button>
  );
}

function SeatCard({ seat, actor, onEdit }: { seat: OrgChartSeat; actor?: SeatActor; onEdit: (seatId: string) => void }) {
  const accent = ACCENT[seatState(seat)];
  const shown = seat.responsibilities.slice(0, MAX_ACCOUNTABILITIES);
  const extra = seat.responsibilities.length - shown.length;

  return (
    <article className={`relative flex flex-col gap-2 overflow-hidden rounded-xl border border-l-4 bg-card p-3 shadow-sm ${CARD} ${accent.bar}`}>
      {/* The whole card is the edit target; the content above it stays inert. */}
      <button type="button" aria-label={`Edit ${seat.name}`} onClick={() => onEdit(seat.id)} className="absolute inset-0 z-0 rounded-xl hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring" />

      <div className="pointer-events-none relative z-10 flex flex-col gap-2 overflow-hidden">
        <h2 className="line-clamp-2 break-words text-sm font-semibold">{seat.name}</h2>

        <div className="grid gap-1">
          <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Name(s)</p>
          {seat.owner_name
            ? <p className="flex items-center gap-1.5 text-xs">
                <ActorAvatar name={actor?.name ?? seat.owner_name} initials={actor?.initials ?? initialsOf(seat.owner_name)} avatarUrl={actor?.avatarUrl} isAgent={seat.owner_type === "agent"} size={20} />
                <span className="truncate">{seat.owner_name}</span>
              </p>
            : <p className={`flex items-center gap-1.5 text-xs font-medium ${accent.label}`}>
                <UserRoundX aria-hidden className="size-4 shrink-0" />Vacant
              </p>}
        </div>

        <div className="grid min-h-0 gap-1">
          <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">Accountabilities</p>
          {shown.length > 0
            ? <ul className="grid list-disc gap-0.5 pl-4 text-xs text-muted-foreground">
                {shown.map((responsibility, index) => <li key={index} className="line-clamp-1 break-words">{responsibility}</li>)}
                {extra > 0 && <li className="list-none text-[11px] font-medium">+{extra} more</li>}
              </ul>
            : <p className="text-xs text-muted-foreground/70">None yet</p>}
        </div>
      </div>
    </article>
  );
}

export function RolesChart({ seats, onEdit, onInsert, resolveActor }: {
  seats: OrgChartSeat[];
  onEdit: (seatId: string) => void;
  onInsert?: (relativeToId: string, mode: InsertMode) => void;
  resolveActor?: (ownerType: "member" | "agent", ownerId: string) => SeatActor | undefined;
}) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const byId = new Map(seats.map((seat) => [seat.id, seat]));
  const ordered = [...seats].sort((a, b) => a.position - b.position || a.name.localeCompare(b.name));
  const children = (parentId: string) => ordered.filter((seat) => seat.parent_id === parentId);
  const roots = ordered.filter((seat) => !seat.parent_id || !byId.has(seat.parent_id));

  // Reachability is computed from the roots, never from what is on screen —
  // otherwise collapsing a parent would push its reports into "Unassigned".
  const reachable = new Set<string>();
  function markReachable(seat: OrgChartSeat, path: Set<string>) {
    reachable.add(seat.id);
    if (path.has(seat.id)) return;
    const nextPath = new Set(path).add(seat.id);
    for (const child of children(seat.id)) markReachable(child, nextPath);
  }
  for (const root of roots) markReachable(root, new Set());

  function toggle(seatId: string) {
    setCollapsed((current) => {
      const next = new Set(current);
      if (!next.delete(seatId)) next.add(seatId);
      return next;
    });
  }

  // Each seat renders as a fixed-height column: card, connector row, then its
  // reports. Because every card and every connector is the same height, the
  // columns line up and a level reads as one row across the whole chart.
  function node(seat: OrgChartSeat, level: number, path: Set<string>) {
    const cyclic = path.has(seat.id);
    const nextPath = new Set(path).add(seat.id);
    const reports = cyclic ? [] : children(seat.id);
    const isCollapsed = collapsed.has(seat.id);
    const showReports = reports.length > 0 && !isCollapsed;
    const actor = seat.owner_id && seat.owner_type ? resolveActor?.(seat.owner_type, seat.owner_id) : undefined;

    return (
      <div key={`${seat.id}-${level}`} role="treeitem" aria-level={level} aria-expanded={reports.length > 0 ? !isCollapsed : undefined} aria-label={seatLabel(seat)} className="flex flex-col items-center">
        <div className="relative">
          <SeatCard seat={seat} actor={actor} onEdit={onEdit} />
          {onInsert && <AddRoleButton seat={seat} mode="above" onInsert={onInsert} className="absolute -top-3 left-1/2 -translate-x-1/2" />}
          {onInsert && <AddRoleButton seat={seat} mode="beside" onInsert={onInsert} className="absolute -right-3 top-1/2 -translate-y-1/2" />}
        </div>

        {cyclic && <p role="alert" className="mt-1 text-xs text-destructive">Reporting cycle needs correction.</p>}

        <div className="mt-1 flex h-7 items-center gap-1">
          {onInsert && <AddRoleButton seat={seat} mode="below" onInsert={onInsert} />}
          {reports.length > 0 && (
            <button
              type="button"
              aria-label={`${isCollapsed ? "Expand" : "Collapse"} ${seat.name}`}
              aria-expanded={!isCollapsed}
              onClick={() => toggle(seat.id)}
              className="relative z-20 grid size-6 place-items-center rounded-full border bg-background text-muted-foreground shadow-sm hover:bg-muted hover:text-foreground"
            >
              {isCollapsed ? <ChevronRight aria-hidden className="size-3.5" /> : <ChevronDown aria-hidden className="size-3.5" />}
            </button>
          )}
        </div>

        {showReports && <span aria-hidden className="h-4 w-px bg-border" />}
        {showReports && (
          <div role="group" className="flex items-start">
            {reports.map((child, index) => (
              <div key={child.id} className="relative flex flex-col items-center px-3 pt-4">
                {reports.length > 1 && <span aria-hidden className={`absolute top-0 h-px bg-border ${index === 0 ? "left-1/2 right-0" : index === reports.length - 1 ? "left-0 right-1/2" : "inset-x-0"}`} />}
                <span aria-hidden className="absolute left-1/2 top-0 h-4 w-px bg-border" />
                {node(child, level + 1, nextPath)}
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  const rootBranches = roots.map((seat) => node(seat, 1, new Set()));
  const unassigned = ordered.filter((seat) => !reachable.has(seat.id));

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Colour legend">
        <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.agent.dot}`} />Agent</span>
        <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.member.dot}`} />Person</span>
        <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.vacant.dot}`} />Vacant seat</span>
      </div>
      <div className="overflow-x-auto pb-4">
        <div role="tree" aria-label="Role hierarchy" className="flex w-max min-w-full items-start gap-8 px-2 pt-4">{rootBranches}</div>
      </div>
      {unassigned.length > 0 && (
        <section aria-label="Unassigned roles" className="grid gap-3 rounded-xl border border-dashed p-4">
          <h2 className="text-sm font-semibold">Unassigned roles</h2>
          <div className="flex flex-wrap items-start gap-8 pt-4">{unassigned.map((seat) => node(seat, 1, new Set()))}</div>
        </section>
      )}
    </div>
  );
}
