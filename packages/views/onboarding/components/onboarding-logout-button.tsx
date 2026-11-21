"use client";

import { LogOut } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useLogout } from "../../auth";
import { useT } from "../../i18n";

/** Account-switch escape hatch in the onboarding header. */
export function OnboardingLogoutButton() {
  const { t } = useT("onboarding");
  const logout = useLogout();

  return (
    <Button
      variant="ghost"
      size="sm"
      className="text-muted-foreground hover:text-foreground"
      onClick={logout}
    >
      <LogOut />
      {t(($) => $.common.log_out)}
    </Button>
  );
}
