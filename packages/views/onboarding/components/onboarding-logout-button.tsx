"use client";

import { LogOut } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useLogout } from "../../auth";
import { useT } from "../../i18n";

/**
 * Account-switch escape hatch shared by every onboarding step.
 *
 * `fixed` only for the welcome screen, which has no rail to sit in. Every
 * other step passes `inline` so it rides the foot of the progress rail:
 * pinning it to the window corner put it outside the measure and above Back,
 * which read as a second header row.
 *
 * Inline means "on the rail". The rail scopes `.dark` to its own panel, so the
 * ordinary muted/foreground tokens already resolve against a dark surface
 * there — no inverted utilities needed. An earlier version hand-mixed
 * `text-background/60` here and rendered as near-invisible grey on black.
 */
export function OnboardingLogoutButton({
  inline = false,
}: {
  /** Render in normal flow (at the foot of the rail) instead of pinned. */
  inline?: boolean;
} = {}) {
  const { t } = useT("onboarding");
  const logout = useLogout();

  return (
    <Button
      variant="ghost"
      size="sm"
      className={cn(
        inline
          ? "-ml-2 w-fit shrink-0 text-muted-foreground hover:text-foreground"
          : "fixed right-8 top-8 z-50 text-muted-foreground hover:text-destructive",
      )}
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      onClick={logout}
    >
      <LogOut />
      {t(($) => $.common.log_out)}
    </Button>
  );
}
