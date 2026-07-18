import { Bot, CircleUserRound, Pencil, UserRoundX } from "lucide-react";

import type { OrgChartSeat } from "../core/types";

export function RolesChart({ seats, onEdit }: { seats: OrgChartSeat[]; onEdit: (seatId: string) => void }) {
  const byId = new Map(seats.map((seat) => [seat.id, seat]));
  const ordered = [...seats].sort((a, b) => a.position - b.position || a.name.localeCompare(b.name));
  const children = (parentId: string) => ordered.filter((seat) => seat.parent_id === parentId);
  const rendered = new Set<string>();

  function branch(seat: OrgChartSeat, level: number, path: Set<string>) {
    const cyclic = path.has(seat.id);
    rendered.add(seat.id);
    const nextPath = new Set(path).add(seat.id);
    const directReports = cyclic ? [] : children(seat.id);
    return <div key={`${seat.id}-${level}`} role="treeitem" aria-level={level} aria-label={`${seat.name}, ${seat.owner_name ? seat.owner_type === "agent" ? `Agent ${seat.owner_name}` : `Owner ${seat.owner_name}` : "Vacant"}`} className="relative grid min-w-0 gap-3 md:justify-items-center">
      <article className="w-full min-w-0 max-w-sm rounded-xl border bg-card p-4 shadow-sm">
        <div className="flex items-start justify-between gap-3"><div className="min-w-0"><h2 className="break-words text-sm font-semibold">{seat.name}</h2><p className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">{seat.owner_name ? seat.owner_type === "agent" ? <><Bot aria-hidden className="size-4 text-primary" />Agent: <strong className="text-foreground">{seat.owner_name}</strong></> : <><CircleUserRound aria-hidden className="size-4" />Owner: <strong className="text-foreground">{seat.owner_name}</strong></> : <><UserRoundX aria-hidden className="size-4 text-amber-600" /><span className="font-medium text-amber-700 dark:text-amber-300">Vacant</span></>}</p></div><button type="button" aria-label={`Edit ${seat.name}`} onClick={() => onEdit(seat.id)} className="grid size-11 shrink-0 place-items-center rounded-md border hover:bg-muted"><Pencil aria-hidden className="size-4" /></button></div>
        {seat.responsibilities.length > 0 && <ul className="mt-3 grid list-disc gap-1 pl-5 text-xs text-muted-foreground">{seat.responsibilities.map((responsibility, index) => <li key={index} className="break-words">{responsibility}</li>)}</ul>}
        {directReports.length > 0 && <p className="mt-3 text-[11px] uppercase tracking-wide text-muted-foreground">{directReports.length} direct {directReports.length === 1 ? "report" : "reports"}</p>}
        {cyclic && <p role="alert" className="mt-3 text-xs text-destructive">Reporting cycle needs correction.</p>}
      </article>
      {directReports.length > 0 && <div aria-hidden className="h-4 w-px bg-border" />}
      {directReports.length > 0 && <div role="group" className="grid w-full min-w-0 gap-4 border-l pl-4 md:grid-flow-col md:auto-cols-fr md:border-l-0 md:border-t md:pt-4">{directReports.map((child) => branch(child, level + 1, nextPath))}</div>}
    </div>;
  }

  const roots = ordered.filter((seat) => !seat.parent_id || !byId.has(seat.parent_id));
  const rootBranches = roots.map((seat) => branch(seat, 1, new Set()));
  const unassigned = ordered.filter((seat) => !rendered.has(seat.id));
  return <div role="tree" aria-label="Role hierarchy" className="grid min-w-0 gap-6 overflow-x-hidden">{rootBranches}{unassigned.length > 0 && <section aria-label="Unassigned roles" className="grid gap-3 rounded-xl border border-dashed p-4"><h2 className="text-sm font-semibold">Unassigned roles</h2>{unassigned.map((seat) => branch(seat, 1, new Set()))}</section>}</div>;
}
