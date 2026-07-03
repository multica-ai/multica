import { Users } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Team } from "@multica/core/types";

/**
 * Team avatar: renders the team's custom icon (emoji, set in the team
 * dialog) when present, otherwise a default glyph on the team color.
 * Custom icon upload / per-team colors are planned; until then every team
 * falls back to the same blue — keep the color here so the future
 * `team.color` field has a single place to land.
 */
export function TeamIcon({ team, className }: { team: Pick<Team, "icon">; className?: string }) {
  return (
    <span
      className={cn(
        "flex size-4 shrink-0 items-center justify-center rounded-sm text-white",
        team.icon ? "bg-transparent text-sm leading-none" : "bg-blue-500",
        className,
      )}
    >
      {team.icon ? team.icon : <Users className="size-3" />}
    </span>
  );
}
