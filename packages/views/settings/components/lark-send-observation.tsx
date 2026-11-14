import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { larkSendObservationOptions } from "@multica/core/lark";
import { useLocale, useT } from "../../i18n";

// Mounted only in the workspace owner/admin management view. The endpoint
// independently enforces that boundary; expanding this performs a read only.
export function LarkSendObservation({ wsId, installationId, revision = "" }: {
  wsId: string;
  installationId: string;
  revision?: string;
}) {
  const { t } = useT("settings");
  const locale = useLocale();
  const [expanded, setExpanded] = useState(false);
  const { data, isPending, isError } = useQuery({
    ...larkSendObservationOptions(wsId, installationId, revision),
    enabled: expanded,
  });
  const observation = data?.observation;
  let status = t(($) => $.lark.send_observation_unavailable);
  if (isPending) status = t(($) => $.lark.loading);
  else if (!isError && data?.available) {
    if (!observation) status = t(($) => $.lark.send_observation_unobserved);
    else switch (observation.result) {
      case "api_confirmed": status = t(($) => $.lark.send_observation_confirmed); break;
      case "rejected": status = t(($) => $.lark.send_observation_rejected); break;
      default: status = t(($) => $.lark.send_observation_uncertain);
    }
  }
  const advice = observation?.result === "rejected" ? (() => {
    switch (observation.reason) {
      case "bot_not_in_chat": return t(($) => $.lark.send_observation_bot_not_in_chat);
      case "bot_disabled": return t(($) => $.lark.send_observation_bot_disabled);
      case "app_unavailable": return t(($) => $.lark.send_observation_app_unavailable);
      case "missing_permission": return t(($) => $.lark.send_observation_missing_permission);
      case "chat_permission": return t(($) => $.lark.send_observation_chat_permission);
      default: return t(($) => $.lark.send_observation_uncertain_hint);
    }
  })() : observation?.result === "uncertain" ? t(($) => $.lark.send_observation_uncertain_hint) : null;

  return (
    <details className="text-caption text-muted-foreground" onToggle={(e) => setExpanded(e.currentTarget.open)}>
      <summary className="cursor-pointer rounded-xs focus-visible:outline focus-visible:outline-ring">
        {t(($) => $.lark.send_observation_title)}
      </summary>
      {expanded && <div className="space-y-1 pt-1" aria-live="polite">
        <p>{status}</p>
        {!isError && data?.available && observation && <>
          <time dateTime={observation.observed_at}>{new Date(observation.observed_at).toLocaleString(locale)}</time>
          {advice && <p>{advice}</p>}
        </>}
        <p>{t(($) => $.lark.send_observation_scope)}</p>
      </div>}
    </details>
  );
}
