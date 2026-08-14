"use client";

import { useConfigStore } from "@multica/core/config";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow } from "./settings-layout";

export function PushoverTab() {
  const { t } = useT("settings");
  const available = useConfigStore((state) => state.pushoverAvailable);

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-body text-muted-foreground">
          {t(($) => $.pushover.integration.section_description)}
        </p>
      </section>
      <SettingsCard>
        <SettingsRow
          label={t(($) =>
            available
              ? $.pushover.integration.enabled_label
              : $.pushover.integration.not_enabled_label,
          )}
          description={t(($) => $.pushover.integration.application_description)}
          align="start"
        >
          <span />
        </SettingsRow>
      </SettingsCard>
    </div>
  );
}
