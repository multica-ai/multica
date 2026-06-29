"use client";

import { useQuery } from "@tanstack/react-query";
import { LarkTab } from "./lark-tab";
import { ComposioTab } from "./composio-tab";
import { ApiError } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useT } from "../../i18n";

// Integrations is the umbrella tab for third-party platform connections.
// GitHub has its own top-level tab (see github-tab.tsx); everything else
// — Lark, Composio, with Slack/Linear etc. to follow — lives in here under
// its own section heading so additional integrations slot in without changing
// the IA. IntegrationsTab is just the host; each integration owns its own
// description and install flow.
export function IntegrationsTab() {
  const { t } = useT("settings");

  // Composio is hidden entirely until a key is configured server-side. A 503
  // from the toolkits endpoint means COMPOSIO_API_KEY is unset; rather than
  // render a card that leaks an internal env-var name to every end user, the
  // whole section (heading + body) is withheld. Admin-only "set this up"
  // guidance is a later, role-gated affordance (MUL-3720 discussion).
  const composioToolkits = useQuery(composioToolkitsOptions());
  const composioUnconfigured =
    composioToolkits.error instanceof ApiError && composioToolkits.error.status === 503;

  return (
    <div className="space-y-10">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.lark.section_title)}</h2>
        <LarkTab />
      </section>
      {!composioUnconfigured && (
        <section className="space-y-4">
          <h2 className="text-sm font-semibold">{t(($) => $.composio.section_title)}</h2>
          <ComposioTab />
        </section>
      )}
    </div>
  );
}
