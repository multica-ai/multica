"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight, Maximize2, Plus, UserRoundX, ZoomIn, ZoomOut } from "lucide-react";
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

// Zoom bounds: 0.2 lets a chart of around a thousand seats fit on screen, 1.5
// lets a single branch read comfortably. Buttons and Ctrl/Cmd-wheel step by this much.
const ZOOM_MIN = 0.2;
const ZOOM_MAX = 1.5;
const ZOOM_STEP = 0.15;
const clampZoom = (value: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(value * 100) / 100));

// One fixed colour code per ownership state so the chart reads at a glance:
// agents are violet, people are sky, and an empty seat stays amber. The class
// shape matches the workspace's categorical-status convention (see HealthBadge).
const ACCENT = {
  agent: { dot: "bg-violet-500", label: "text-violet-700 dark:text-violet-300" },
  member: { dot: "bg-sky-500", label: "text-sky-700 dark:text-sky-300" },
  vacant: { dot: "bg-amber-500", label: "text-amber-700 dark:text-amber-300" },
} as const;

function initialsOf(name: string): string {
  return name.trim().split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase() ?? "").join("") || "?";
}

// Every card is the same width so a level reads as one row of equal columns,
// and its height follows its content — the shape of the org-chart reference.
// A fixed height made a two-line seat as tall as a six-line one, which is what
// made the chart far too big to hold a real org (FIR-3589 items 10 and 12).
const CARD = "w-56 shrink-0";

function seatLabel(seat: OrgChartSeat): string {
  if (seat.owners.length === 0) return `${seat.name}, Vacant`;
  return `${seat.name}, held by ${seat.owners.map((owner) => owner.name || "Unnamed").join(", ")}`;
}

// The small round + that sits on a connector line, one per direction. Kept
// visually tiny per the org-chart design, with a padded hit area for touch.
// It stays faintly visible at all times and only brightens on hover: a
// hover-only reveal makes it unreachable on a phone, where the button simply
// never appears (FIR-3589 item 8).
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
      className={`relative z-20 grid size-6 place-items-center rounded-full border bg-background text-muted-foreground opacity-60 shadow-sm transition-opacity before:absolute before:-inset-2 before:content-[''] hover:bg-muted hover:text-foreground hover:opacity-100 focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 ${className ?? ""}`}
    >
      <Plus aria-hidden className="size-3.5" />
    </button>
  );
}

function SeatCard({ seat, resolveActor, onEdit }: {
  seat: OrgChartSeat;
  resolveActor?: (ownerType: "member" | "agent", ownerId: string) => SeatActor | undefined;
  onEdit: (seatId: string) => void;
}) {
  return (
    <article className={`relative flex flex-col gap-2 rounded-lg border bg-card p-3 shadow-sm ${CARD}`}>
      {/* The whole card is the edit target; the content above it stays inert. */}
      <button type="button" aria-label={`Edit ${seat.name}`} onClick={() => onEdit(seat.id)} className="absolute inset-0 z-0 rounded-lg hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring" />

      <div className="pointer-events-none relative z-10 flex flex-col gap-2">
        <h2 className="break-words text-[13px] font-semibold leading-tight">{seat.name}</h2>

        <div className="grid gap-1">
          <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">Name(s)</p>
          {seat.owners.length > 0
            ? <ul className="grid gap-1">
                {seat.owners.map((owner) => {
                  const actor = resolveActor?.(owner.type, owner.id);
                  const name = actor?.name ?? owner.name ?? "Unnamed";
                  return (
                    <li key={`${owner.type}:${owner.id}`} className="flex items-center gap-1.5 text-xs">
                      <ActorAvatar name={name} initials={actor?.initials ?? initialsOf(name)} avatarUrl={actor?.avatarUrl} isAgent={owner.type === "agent"} size={18} />
                      <span aria-hidden className={`size-1.5 shrink-0 rounded-full ${ACCENT[owner.type].dot}`} />
                      <span className="truncate">{name}</span>
                    </li>
                  );
                })}
              </ul>
            : <p className={`flex items-center gap-1.5 text-xs font-medium ${ACCENT.vacant.label}`}>
                <UserRoundX aria-hidden className="size-3.5 shrink-0" />Vacant
              </p>}
        </div>

        {seat.responsibilities.length > 0 && (
          <div className="grid gap-1">
            <p className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground">Accountabilities</p>
            <ul className="grid list-disc gap-0.5 pl-4 text-[11px] leading-snug text-muted-foreground">
              {seat.responsibilities.map((responsibility, index) => <li key={index} className="break-words">{responsibility}</li>)}
            </ul>
          </div>
        )}
      </div>
    </article>
  );
}

// The zoom rail: explicit buttons so zoom never depends on a wheel gesture, plus
// a readable percentage. Ctrl/Cmd-wheel over the canvas zooms too (wired in the
// viewport), but the buttons are the guaranteed path.
function ZoomControls({ scale, onZoom, onReset }: { scale: number; onZoom: (delta: number) => void; onReset: () => void }) {
  return (
    <div className="flex items-center gap-1 rounded-lg border bg-background p-1 shadow-sm">
      <button type="button" aria-label="Zoom out" onClick={() => onZoom(-ZOOM_STEP)} disabled={scale <= ZOOM_MIN} className="grid size-8 place-items-center rounded-md hover:bg-muted disabled:opacity-40"><ZoomOut aria-hidden className="size-4" /></button>
      <span className="min-w-11 text-center text-xs font-medium tabular-nums text-muted-foreground" aria-live="polite">{Math.round(scale * 100)}%</span>
      <button type="button" aria-label="Zoom in" onClick={() => onZoom(ZOOM_STEP)} disabled={scale >= ZOOM_MAX} className="grid size-8 place-items-center rounded-md hover:bg-muted disabled:opacity-40"><ZoomIn aria-hidden className="size-4" /></button>
      <button type="button" aria-label="Reset zoom" onClick={onReset} className="grid size-8 place-items-center rounded-md hover:bg-muted"><Maximize2 aria-hidden className="size-4" /></button>
    </div>
  );
}

export function RolesChart({ seats, onEdit, onInsert, resolveActor }: {
  seats: OrgChartSeat[];
  onEdit: (seatId: string) => void;
  onInsert?: (relativeToId: string, mode: InsertMode) => void;
  resolveActor?: (ownerType: "member" | "agent", ownerId: string) => SeatActor | undefined;
}) {
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [scale, setScale] = useState(1);
  const viewportRef = useRef<HTMLDivElement>(null);
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

  const zoomBy = (delta: number) => setScale((current) => clampZoom(current + delta));

  // Ctrl/Cmd-wheel zooms the canvas. React's onWheel is passive, so preventing
  // the browser's own page zoom needs a native non-passive listener.
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const onWheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      setScale((current) => clampZoom(current - Math.sign(event.deltaY) * ZOOM_STEP));
    };
    viewport.addEventListener("wheel", onWheel, { passive: false });
    return () => viewport.removeEventListener("wheel", onWheel);
  }, []);

  // Drag anywhere on the empty canvas to pan; dragging a card or a + button does
  // its own thing. Pointer capture keeps the pan smooth past the viewport edge.
  const pan = useRef<{ x: number; y: number; left: number; top: number } | null>(null);
  const [panning, setPanning] = useState(false);
  function onPointerDown(event: React.PointerEvent<HTMLDivElement>) {
    const viewport = viewportRef.current;
    if (!viewport || event.button !== 0) return;
    if ((event.target as HTMLElement).closest("button, article, a, input")) return;
    pan.current = { x: event.clientX, y: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop };
    setPanning(true);
    viewport.setPointerCapture(event.pointerId);
  }
  function onPointerMove(event: React.PointerEvent<HTMLDivElement>) {
    const viewport = viewportRef.current;
    if (!viewport || !pan.current) return;
    viewport.scrollLeft = pan.current.left - (event.clientX - pan.current.x);
    viewport.scrollTop = pan.current.top - (event.clientY - pan.current.y);
  }
  function endPan(event: React.PointerEvent<HTMLDivElement>) {
    if (!pan.current) return;
    pan.current = null;
    setPanning(false);
    viewportRef.current?.releasePointerCapture(event.pointerId);
  }

  // Each seat renders as a fixed-height column: card, connector row, then its
  // reports. Because every card and every connector is the same height, the
  // columns line up and a level reads as one row across the whole chart. The
  // per-card `group` scopes the hover-reveal of the + buttons to just that seat.
  function node(seat: OrgChartSeat, level: number, path: Set<string>) {
    const cyclic = path.has(seat.id);
    const nextPath = new Set(path).add(seat.id);
    const reports = cyclic ? [] : children(seat.id);
    const isCollapsed = collapsed.has(seat.id);
    const showReports = reports.length > 0 && !isCollapsed;

    return (
      <div key={`${seat.id}-${level}`} role="treeitem" aria-level={level} aria-expanded={reports.length > 0 ? !isCollapsed : undefined} aria-label={seatLabel(seat)} className="flex flex-col items-center">
        <div className="group flex flex-col items-center">
          <div className="relative">
            <SeatCard seat={seat} resolveActor={resolveActor} onEdit={onEdit} />
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
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Colour legend">
          <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.agent.dot}`} />Agent</span>
          <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.member.dot}`} />Person</span>
          <span className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-full ${ACCENT.vacant.dot}`} />Vacant seat</span>
        </div>
        <ZoomControls scale={scale} onZoom={zoomBy} onReset={() => setScale(1)} />
      </div>
      {/* The viewport clips and scrolls; the canvas inside is scaled and can be
          dragged to pan, so a chart of several hundred seats stays navigable. */}
      <div
        ref={viewportRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endPan}
        onPointerCancel={endPan}
        aria-label="Role chart canvas"
        className={`relative h-[70vh] min-h-96 overflow-auto rounded-xl border bg-muted/20 ${panning ? "cursor-grabbing select-none" : "cursor-grab"}`}
      >
        <div className="w-max origin-top-left p-8 transition-transform" style={{ transform: `scale(${scale})` }}>
          <div role="tree" aria-label="Role hierarchy" className="flex items-start gap-8">{rootBranches}</div>
          {unassigned.length > 0 && (
            <section aria-label="Unassigned roles" className="mt-6 grid gap-3 rounded-xl border border-dashed bg-background/60 p-4">
              <h2 className="text-sm font-semibold">Unassigned roles</h2>
              <div className="flex flex-wrap items-start gap-8 pt-4">{unassigned.map((seat) => node(seat, 1, new Set()))}</div>
            </section>
          )}
        </div>
      </div>
    </div>
  );
}
