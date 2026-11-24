"use client";

import { AlertTriangle, Plug } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { Agent } from "@multica/core/types";
import { ObjectProperty } from "../../layout/object-page";
import { useT } from "../../i18n";
import { agentAppsSectionId, needsViewerAccount } from "./agent-app-status";
import { CHIP_CLASS } from "./inspector/chip";
import { useAgentApps } from "./use-agent-apps";

/** The agent's apps in one line, leading with the ones the viewer still has
 * to connect; it takes the viewer to the Apps section. */
export function AgentAppsProperty({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const { rows, isLoading, isError } = useAgentApps(agent.id);
  const missing = rows.filter((row) => needsViewerAccount(row.app.account_scope, row.status)).length;

  const value = isLoading || isError
    ? "—"
    : missing > 0
      ? t(($) => $.panel.apps_needs_account, { count: missing })
      : rows.length > 0
        ? t(($) => $.panel.apps_count, { count: rows.length })
        : t(($) => $.panel.apps_none);

  const show = () => {
    const section = document.getElementById(agentAppsSectionId(agent.id));
    section?.scrollIntoView({ block: "start", behavior: "smooth" });
    section?.focus({ preventScroll: true });
  };

  const Icon = missing > 0 ? AlertTriangle : Plug;
  return (
    <ObjectProperty label={t(($) => $.panel.apps)}>
      <button
        type="button"
        className={cn(CHIP_CLASS, missing > 0 && "text-warning")}
        onClick={show}
        aria-label={t(($) => $.panel.apps_aria, { value })}
      >
        <Icon
          className={cn("size-3.5 shrink-0", missing > 0 ? "text-warning" : "text-muted-foreground")}
          aria-hidden="true"
        />
        <span className="truncate">{value}</span>
      </button>
    </ObjectProperty>
  );
}
