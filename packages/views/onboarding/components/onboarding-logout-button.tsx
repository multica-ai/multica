"use client";

import { LogOut } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useLogout } from "../../auth";
import { useT } from "../../i18n";

/**
 * Account-switch escape hatch shared by every onboarding step.
 *
 * The fixed position keeps it available across the flow's full-bleed layouts,
 * including the narrow mobile layout where the issue was originally reported.
 */
export function OnboardingLogoutButton() {
  const { t } = useT("onboarding");
  const logout = useLogout();

  return (
    <Button
      variant="ghost"
      size="sm"
      className="fixed right-8 top-8 z-50 text-muted-foreground hover:text-destructive"
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      onClick={logout}
    >
      <LogOut />
      {t(($) => $.common.log_out)}
    </Button>
  );
}
