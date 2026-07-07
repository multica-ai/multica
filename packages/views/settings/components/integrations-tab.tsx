"use client";

import { useQuery } from "@tanstack/react-query";
import { LarkTab } from "./lark-tab";
import { ComposioTab } from "./composio-tab";
import { SlackTab } from "./slack-tab";
import { ApiError } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useT } from "../../i18n";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — currently Lark, Composio, and Slack, with Linear etc. to follow — lives in
// here under its own section heading so additional integrations slot in without
// changing the IA. IntegrationsTab is just the host; each integration owns its
// own description and install flow.
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
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>
      {composioEnabled && !composioUnconfigured && (
        <section className="space-y-4">
          <h2 className="text-sm font-semibold">{t(($) => $.composio.section_title)}</h2>
          <ComposioTab />
        </section>
      )}
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.slack.section_title)}</h2>
        <SlackTab />
      </section>
    </div>
  );
}
