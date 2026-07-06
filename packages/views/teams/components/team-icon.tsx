import { Users } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Team } from "@multica/core/types";

/**
 * Team avatar: renders the team's custom icon (emoji, set in the team
 * dialog) when present, otherwise a default glyph on a neutral block.
 * Custom icon upload / per-team colors are planned — this fallback block
 * is where the future `team.color` field lands; until then it stays on
 * semantic neutrals so it reads as "unset" rather than a chosen color.
 */
export function TeamIcon({ team, className }: { team: Pick<Team, "icon">; className?: string }) {
  return (
    <span
      className={cn(
        "flex size-4 shrink-0 items-center justify-center rounded-sm",
        team.icon
          ? "bg-transparent text-sm leading-none"
          : "bg-muted text-muted-foreground",
        className,
      )}
    >
      {team.icon ? team.icon : <Users className="size-3" />}
    </span>
  );
}
