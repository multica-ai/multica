"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import type { OfficeFloor } from "@multica/core/office";
import type { Agent, Squad } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { PAGE_GUTTER } from "../../layout/page-header";
import { OfficeHomeSquadMenu } from "./office-home-squad-menu";
import { OfficeSquadSeatMenu } from "./office-squad-seat-menu";

const CHIP =
  "h-6 max-w-48 rounded-full border bg-background px-2.5 text-caption hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring";

/** One line for the owner or an admin when squads have no room: seat them all in one click. */
export function OfficeSeatBanner({
  count,
  pending,
  onSeatAll,
}: {
  count: number;
  pending: boolean;
  onSeatAll: () => void;
}) {
  const { t } = useT("issues");
  return (
    <div className="mx-4 mb-2 flex shrink-0 items-center gap-2 rounded-lg bg-muted/50 px-3 py-1.5 text-caption text-muted-foreground">
      <span className="min-w-0 flex-1">{t(($) => $.office.seat_banner, { count })}</span>
      <Button type="button" variant="outline" size="xs" disabled={pending} aria-busy={pending} onClick={onSeatAll}>
        {t(($) => $.office.seat_all)}
      </Button>
    </div>
  );
}

/**
 * A collapsible strip of what the floors do not draw: squads with no seat and
 * agents with no home squad. A menu on each squad seats it; a menu on each
 * agent sets its home squad.
 */
export function OfficeUnseatedTray({
  squads,
  agents,
  floors,
  allSquads,
  canSeat,
}: {
  squads: readonly Squad[];
  agents: readonly Agent[];
  floors: readonly OfficeFloor[];
  /** Every squad, to tell which rooms are free. */
  allSquads: readonly Squad[];
  /** Whether the viewer may move squads (the owner or an admin). */
  canSeat: (squad: Squad) => boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  if (squads.length === 0 && agents.length === 0) return null;
  return (
    <section aria-label={t(($) => $.office.unseated)} className={cn("shrink-0 border-t py-2", PAGE_GUTTER)}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1 text-caption text-muted-foreground hover:text-foreground"
      >
        {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        {t(($) => $.office.unseated)}
        <span className="tabular-nums">{`${squads.length + agents.length}`}</span>
      </button>
      {open ? (
        <div className="mt-2 flex max-h-28 flex-wrap gap-1.5 overflow-y-auto">
          {squads.map((squad) => (
            <SquadChip key={squad.id} squad={squad} floors={floors} allSquads={allSquads} canSeat={canSeat(squad)} />
          ))}
          {agents.map((agent) => (
            <OfficeHomeSquadMenu
              key={agent.id}
              agent={agent}
              align="start"
              trigger={
                <Button type="button" variant="ghost" className={CHIP} aria-label={t(($) => $.office.unseated_agent, { name: agent.name })}>
                  <span className="truncate">{agent.name}</span>
                </Button>
              }
            />
          ))}
        </div>
      ) : null}
    </section>
  );
}

function SquadChip({
  squad,
  floors,
  allSquads,
  canSeat,
}: {
  squad: Squad;
  floors: readonly OfficeFloor[];
  allSquads: readonly Squad[];
  canSeat: boolean;
}) {
  const { t } = useT("issues");

  const label = <span className="truncate">{squad.name}</span>;
  if (!canSeat) {
    return <span className={cn(CHIP, "inline-flex items-center")}>{label}</span>;
  }
  return (
    <OfficeSquadSeatMenu
      squad={squad}
      floors={floors}
      allSquads={allSquads}
      trigger={
        <Button type="button" variant="ghost" className={CHIP} aria-label={t(($) => $.office.unseated_squad, { name: squad.name })} />
      }
    >
      {label}
    </OfficeSquadSeatMenu>
  );
}
