"use client";

import { useState } from "react";
import { useConfigStore } from "@multica/core/config";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { TOTPSetupDialog } from "./totp-setup-dialog";
import { TOTPDisableDialog } from "./totp-disable-dialog";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

export function SecurityTab() {
  const { t } = useT("settings");
  const totpSupported = useConfigStore((s) => s.totpSupported);
  const [setupOpen, setSetupOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);

  // Read current user's TOTP state via getMe(). totp_enabled is added to the
  // /api/me response in Task 10's server-side change (UserResponse.TotpEnabled).
  // The field is optional (older servers won't return it) so we default to
  // false on missing/undefined — the "Set up" CTA is the safe fallback.
  const { data: me, refetch } = useQuery({
    queryKey: ["me-totp"],
    queryFn: () => api.getMe(),
    // An admin can reset the authenticator from another session; refetch
    // whenever the tab mounts so a stale "Disable" cannot hide "Set up".
    refetchOnMount: "always",
  });
  const enabled = me?.totp_enabled === true;

  return (
    <SettingsTab title={t(($) => $.page.tabs.security)} scope="account">
      <SettingsSection
        title={t(($) => $.security.title)}
        description={t(($) => $.security.description)}
      >
        <SettingsCard>
          <SettingsRow
            anchor="authenticator"
            label={t(($) => $.security.two_factor.label)}
            description={
              !totpSupported
                ? t(($) => $.security.two_factor.hint_unavailable)
                : enabled
                  ? t(($) => $.security.two_factor.hint_enabled)
                  : t(($) => $.security.two_factor.hint_disabled)
            }
          >
            {totpSupported ? (
              enabled ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setDisableOpen(true)}
                >
                  {t(($) => $.security.two_factor.disable_button)}
                </Button>
              ) : (
                <Button size="sm" onClick={() => setSetupOpen(true)}>
                  {t(($) => $.security.two_factor.setup_button)}
                </Button>
              )
            ) : (
              <span className="shrink-0 text-xs font-medium text-muted-foreground">
                {t(($) => $.security.two_factor.unavailable_badge)}
              </span>
            )}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <TOTPSetupDialog
        open={setupOpen}
        onOpenChange={setSetupOpen}
        onSuccess={() => refetch()}
      />
      <TOTPDisableDialog
        open={disableOpen}
        onOpenChange={setDisableOpen}
        onSuccess={() => refetch()}
      />
    </SettingsTab>
  );
}
