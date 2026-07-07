import { Users } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import type { Space } from "@multica/core/types";

/**
 * Space avatar: renders the space's custom icon (emoji, set in the space
 * dialog) when present, otherwise a default glyph on a neutral block.
 * Custom icon upload / per-space colors are planned — this fallback block
 * is where the future `space.color` field lands; until then it stays on
 * semantic neutrals so it reads as "unset" rather than a chosen color.
 */
export function SpaceIcon({ space, className }: { space: Pick<Space, "icon">; className?: string }) {
  return (
    <span
      className={cn(
        "flex size-4 shrink-0 items-center justify-center rounded-sm",
        space.icon
          ? "bg-transparent text-sm leading-none"
          : "bg-muted text-muted-foreground",
        className,
      )}
    >
      {space.icon ? space.icon : <Users className="size-3" />}
    </span>
  );
}
