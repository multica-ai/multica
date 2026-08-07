"use client";

import { useConfigStore } from "@multica/core/config";
import { ExternalLink } from "lucide-react";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow } from "./settings-layout";

export function PushoverTab() {
  const { t } = useT("settings");
  const available = useConfigStore((state) => state.pushoverAvailable);

  if (!available) return null;

  return (
    <SettingsCard>
      <SettingsRow
        label={t(($) => $.pushover.integration.application_label)}
        description={t(($) => $.pushover.integration.application_description)}
        align="start"
      >
        <div className="space-y-2 text-left sm:text-right">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.pushover.integration.configured)}
          </p>
          <a
            className="inline-flex items-center gap-1 text-caption text-primary hover:underline"
            href="https://pushover.net/apps/build"
            target="_blank"
            rel="noreferrer"
          >
            {t(($) => $.pushover.integration.create_application)}
            <ExternalLink className="size-3" />
          </a>
        </div>
      </SettingsRow>
    </SettingsCard>
  );
}
