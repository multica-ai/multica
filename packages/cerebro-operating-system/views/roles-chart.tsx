"use client";

import { useState } from "react";
import { ArrowDown, ArrowRight, ArrowUp, Bot, CircleUserRound, Pencil, Plus, UserRoundX, X } from "lucide-react";
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

const MAX_RESPONSIBILITIES = 3;

function InsertControls({ seat, onInsert }: { seat: OrgChartSeat; onInsert: (relativeToId: string, mode: InsertMode) => void }) {
  const [open, setOpen] = useState(false);
  const directions: { mode: InsertMode; label: string; icon: typeof ArrowUp }[] = [
    { mode: "above", label: "above", icon: ArrowUp },
    { mode: "beside", label: "beside", icon: ArrowRight },
    { mode: "below", label: "below", icon: ArrowDown },
  ];
  return (
    <div className="relative">
      <button
        type="button"
        aria-label={`Add a role near ${seat.name}`}
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex min-h-9 items-center gap-1 rounded-md border px-2 text-xs font-medium text-muted-foreground hover:bg-muted"
      >
        {open ? <X aria-hidden className="size-3.5" /> : <Plus aria-hidden className="size-3.5" />}
        Add role
      </button>
      {open && (
        <div role="group" aria-label={`Where to add a role near ${seat.name}`} className="absolute left-0 top-full z-10 mt-1 grid gap-1 rounded-md border bg-popover p-1 shadow-md">
          {directions.map(({ mode, label, icon: Icon }) => (
            <button
              key={mode}
              type="button"
              aria-label={`Add role ${label} ${seat.name}`}
              onClick={() => { onInsert(seat.id, mode); setOpen(false); }}
              className="flex min-h-9 items-center gap-2 rounded px-2 text-left text-xs font-medium hover:bg-muted"
            >
              <Icon aria-hidden className="size-3.5 text-muted-foreground" />
              <span className="capitalize">{label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function RolesChart({ seats, onEdit, onInsert, resolveActor }: {
  seats: OrgChartSeat[];
  onEdit: (seatId: string) => void;
  onInsert?: (relativeToId: string, mode: InsertMode) => void;
  resolveActor?: (ownerType: "member" | "agent", ownerId: string) => SeatActor | undefined;
}) {
  const byId = new Map(seats.map((seat) => [seat.id, seat]));
  const ordered = [...seats].sort((a, b) => a.position - b.position || a.name.localeCompare(b.name));
  const children = (parentId: string) => ordered.filter((seat) => seat.parent_id === parentId);
  const rendered = new Set<string>();

  function branch(seat: OrgChartSeat, level: number, path: Set<string>) {
    const cyclic = path.has(seat.id);
    rendered.add(seat.id);
    const nextPath = new Set(path).add(seat.id);
    const directReports = cyclic ? [] : children(seat.id);
    const state = seatState(seat);
    const accent = ACCENT[state];
    const actor = seat.owner_id && seat.owner_type ? resolveActor?.(seat.owner_type, seat.owner_id) : undefined;
    const extraResponsibilities = seat.responsibilities.length - MAX_RESPONSIBILITIES;

    return <div key={`${seat.id}-${level}`} role="treeitem" aria-level={level} aria-label={`${seat.name}, ${seat.owner_name ? seat.owner_type === "agent" ? `Agent ${seat.owner_name}` : `Owner ${seat.owner_name}` : "Vacant"}`} className="relative grid min-w-0 gap-3 md:justify-items-center">
      <article className={`flex min-h-40 w-full min-w-0 flex-col gap-3 rounded-xl border border-l-4 bg-card p-4 shadow-sm md:w-72 ${accent.bar}`}>
        <div className="flex items-start gap-3">
          {seat.owner_name
            ? <ActorAvatar name={actor?.name ?? seat.owner_name} initials={actor?.initials ?? initialsOf(seat.owner_name)} avatarUrl={actor?.avatarUrl} isAgent={seat.owner_type === "agent"} size={36} />
            : <div aria-hidden className="grid size-9 shrink-0 place-items-center rounded-full bg-muted text-muted-foreground"><UserRoundX className="size-4" /></div>}
          <div className="min-w-0 flex-1">
            <h2 className="line-clamp-2 break-words text-sm font-semibold">{seat.name}</h2>
            <p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
              <span aria-hidden className={`size-2 shrink-0 rounded-full ${accent.dot}`} />
              {seat.owner_name
                ? seat.owner_type === "agent"
                  ? <><Bot aria-hidden className="size-3.5 shrink-0" /><span className="truncate">Agent: <strong className="text-foreground">{seat.owner_name}</strong></span></>
                  : <><CircleUserRound aria-hidden className="size-3.5 shrink-0" /><span className="truncate">Owner: <strong className="text-foreground">{seat.owner_name}</strong></span></>
                : <span className={`font-medium ${accent.label}`}>Vacant</span>}
            </p>
          </div>
          <button type="button" aria-label={`Edit ${seat.name}`} onClick={() => onEdit(seat.id)} className="grid size-9 shrink-0 place-items-center rounded-md border hover:bg-muted"><Pencil aria-hidden className="size-4" /></button>
        </div>

        {seat.responsibilities.length > 0 && <ul className="grid list-disc gap-1 pl-5 text-xs text-muted-foreground">
          {seat.responsibilities.slice(0, MAX_RESPONSIBILITIES).map((responsibility, index) => <li key={index} className="line-clamp-1 break-words">{responsibility}</li>)}
          {extraResponsibilities > 0 && <li className="list-none text-[11px] font-medium text-muted-foreground/80">+{extraResponsibilities} more</li>}
        </ul>}

        <div className="mt-auto flex items-center justify-between gap-2 pt-1">
          <span className="text-[11px] uppercase tracking-wide text-muted-foreground">{directReports.length > 0 ? `${directReports.length} direct ${directReports.length === 1 ? "report" : "reports"}` : "No reports"}</span>
          {onInsert && <InsertControls seat={seat} onInsert={onInsert} />}
        </div>

        {cyclic && <p role="alert" className="text-xs text-destructive">Reporting cycle needs correction.</p>}
      </article>
      {directReports.length > 0 && <div aria-hidden className="h-4 w-px bg-border" />}
      {directReports.length > 0 && <div role="group" className="grid w-full min-w-0 gap-4 border-l pl-4 md:grid-flow-col md:auto-cols-fr md:justify-items-center md:border-l-0 md:border-t md:pl-0 md:pt-4">{directReports.map((child) => branch(child, level + 1, nextPath))}</div>}
    </div>;
  }

  const roots = ordered.filter((seat) => !seat.parent_id || !byId.has(seat.parent_id));
  const rootBranches = roots.map((seat) => branch(seat, 1, new Set()));
  const unassigned = ordered.filter((seat) => !rendered.has(seat.id));
  return <div className="grid gap-4">
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Colour legend">
      <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.agent.dot}`} />Agent</span>
      <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.member.dot}`} />Person</span>
      <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.vacant.dot}`} />Vacant seat</span>
    </div>
    <div role="tree" aria-label="Role hierarchy" className="grid min-w-0 gap-6 overflow-x-auto pb-2">{rootBranches}{unassigned.length > 0 && <section aria-label="Unassigned roles" className="grid gap-3 rounded-xl border border-dashed p-4"><h2 className="text-sm font-semibold">Unassigned roles</h2>{unassigned.map((seat) => branch(seat, 1, new Set()))}</section>}</div>
  </div>;
}
