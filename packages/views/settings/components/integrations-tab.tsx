"use client";

import { useQuery } from "@tanstack/react-query";
import { LarkTab } from "./lark-tab";
import { ComposioTab } from "./composio-tab";
import { SlackTab } from "./slack-tab";
import { VCSTab } from "./vcs-tab";
import { WecomTab } from "./wecom-tab";
import { ApiError } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useConfigStore, useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useT } from "../../i18n";
import { SettingsSection, SettingsTab } from "./settings-layout";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — currently Lark, Composio, Slack, the self-hosted Git providers (Forgejo /
// Gitea / GitLab), and WeChat Work smart-bot, with Linear etc. to follow —
// lives in here under its own section heading so additional integrations slot
// in without changing the IA. IntegrationsTab is just the host; each
// integration owns its own description and install flow.
export function IntegrationsTab() {
  const { t } = useT("settings");

  const composioEnabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const composioToolkits = useQuery({
    ...composioToolkitsOptions(),
    enabled: composioEnabled,
  });
  const composioUnconfigured =
    composioToolkits.error instanceof ApiError && composioToolkits.error.status === 503;

  // Self-host-only integration: the managed cloud reports this false (field
  // omitted from /api/config), so the whole section — header included — is
  // hidden there rather than showing an operator-only "missing key" message.
  const vcsAvailable = useConfigStore((s) => s.vcsIntegrationAvailable);

  return (
    <SettingsTab title={t(($) => $.page.tabs.integrations)}>
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
      {vcsAvailable && (
        <SettingsSection title={t(($) => $.vcs.section_title)}>
          <VCSTab />
        </SettingsSection>
      )}
      <SettingsSection title={t(($) => $.wecom.section_title)}>
        <WecomTab />
      </SettingsSection>
    </SettingsTab>
  );
}
