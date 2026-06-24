"use client";

import { useState } from "react";
import { useConfigStore } from "@multica/core/config";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { TOTPSetupDialog } from "./totp-setup-dialog";
import { TOTPDisableDialog } from "./totp-disable-dialog";

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
  });
  const enabled = me?.totp_enabled === true;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.security.title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.security.description)}
          </p>
        </div>
        <Card>
          <CardContent>
            <div className="flex items-center justify-between gap-4">
              <div className="space-y-0.5 pr-4">
                <p className="text-sm font-medium">
                  {t(($) => $.security.two_factor.label)}
                </p>
                <p className="text-xs text-muted-foreground">
                  {!totpSupported
                    ? t(($) => $.security.two_factor.hint_unavailable)
                    : enabled
                      ? t(($) => $.security.two_factor.hint_enabled)
                      : t(($) => $.security.two_factor.hint_disabled)}
                </p>
              </div>
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
            </div>
          </CardContent>
        </Card>
      </section>

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
    </div>
  );
}
