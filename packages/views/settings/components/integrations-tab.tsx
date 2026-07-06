"use client";

import { LarkTab } from "./lark-tab";
import { SlackTab } from "./slack-tab";
import { useT } from "../../i18n";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — currently Lark and Slack, with Linear etc. to follow — lives in here
// under its own section heading so additional integrations slot in without
// changing the IA. IntegrationsTab is just the host; each integration owns
// its own description and install flow.
export function IntegrationsTab() {
  const { t } = useT("settings");
  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.slack.section_title)}</h2>
        <SlackTab />
      </section>
    </div>
  );
}
