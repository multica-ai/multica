"use client";

import { useQuery } from "@tanstack/react-query";
import { LarkTab } from "./lark-tab";
import { ComposioTab } from "./composio-tab";
import { SlackTab } from "./slack-tab";
import { GitHubTab } from "./github-tab";
import { ApiError } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useT } from "../../i18n";
import { SettingsSection, SettingsTab } from "./settings-layout";

// Integrations is the single home for third-party platform connections. Each
// integration owns its description and install flow; adding another provider
// does not add another top-level Settings destination.
export function IntegrationsTab() {
  const { t } = useT("settings");

  const composioEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  // Composio is hidden entirely until the feature is enabled and a key is
  // configured server-side. A 503 from the toolkits endpoint means the server
  // withheld the integration despite the frontend flag being on.
  const composioToolkits = useQuery({
    ...composioToolkitsOptions(),
    enabled: composioEnabled,
  });
  const composioUnconfigured =
    composioToolkits.error instanceof ApiError && composioToolkits.error.status === 503;

  return (
    <SettingsTab title={t(($) => $.page.tabs.integrations)}>
      <SettingsSection title={t(($) => $.github.connection_title)}>
        <GitHubTab />
      </SettingsSection>
      <SettingsSection title={t(($) => $.lark.section_title)}>
        <LarkTab />
      </SettingsSection>
      {composioEnabled && !composioUnconfigured && (
        <SettingsSection title={t(($) => $.composio.section_title)}>
          <ComposioTab />
        </SettingsSection>
      )}
      <SettingsSection title={t(($) => $.slack.section_title)}>
        <SlackTab />
      </SettingsSection>
    </SettingsTab>
  );
}
