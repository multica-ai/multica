"use client";

import { useMemo, useState } from "react";
import type { AppAccountScope, IssueAppCall } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";
import { OutcomeIcon, outcomeLabel, toolLabel, useIssueAppCalls } from "./app-call-chips";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

type AppGroup = {
  key: string;
  toolkit: string;
  scope: AppAccountScope;
  calls: IssueAppCall[];
  tools: Map<string, number>;
};

/** Outcomes worth calling out next to an app; plain successes are not. */
const NOTABLE = ["pending", "blocked", "rejected", "error", "rate_limited"] as const;

/**
 * "Apps" in the issue sidebar: which apps, through which account, this
 * issue's runs called, so a reader sees what an agent touched outside
 * Multica without opening every run. Renders nothing until a call is made.
 */
export function IssueAppsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const { calls, appName } = useIssueAppCalls(issueId);
  const groups = useMemo(() => {
    const byApp = new Map<string, AppGroup>();
    for (const call of calls) {
      const key = `${call.account_scope}:${call.toolkit_slug}`;
      let group = byApp.get(key);
      if (!group) {
        group = { key, toolkit: call.toolkit_slug, scope: call.account_scope, calls: [], tools: new Map() };
        byApp.set(key, group);
      }
      group.calls.push(call);
      group.tools.set(call.tool_slug, (group.tools.get(call.tool_slug) ?? 0) + 1);
    }
    return [...byApp.values()];
  }, [calls]);

  if (groups.length === 0) return null;

  return (
    <div>
      <IssueSidebarSectionHeader
        title={t(($) => $.app_calls.section_title)}
        open={open}
        onOpenChange={setOpen}
        count={groups.length}
      />
      {open && (
        <div className="px-2">
          {groups.map((group) => {
            const name = appName(group.toolkit);
            const notable = NOTABLE.flatMap((outcome) => {
              const count = group.calls.filter((c) => c.outcome === outcome).length;
              return count > 0 ? [{ outcome, count }] : [];
            });
            const tools = [...group.tools.entries()]
              .map(([tool, count]) => (count > 1 ? `${toolLabel(group.toolkit, tool)} ×${count}` : toolLabel(group.toolkit, tool)))
              .join(", ");
            return (
              <div key={group.key} className="flex items-start gap-2 rounded-md px-2 py-2 text-caption" title={tools}>
                <ComposioToolkitLogo slug={group.toolkit} name={name} className="mt-px size-4 rounded-[3px] text-[9px]" />
                <div className="min-w-0 flex-1">
                  <p className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate font-medium">{name}</span>
                    <span className="shrink-0 text-muted-foreground">{t(($) => $.app_calls.scope[group.scope])}</span>
                  </p>
                  <p className="truncate text-muted-foreground">{tools}</p>
                  {notable.length > 0 && (
                    <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-muted-foreground">
                      {notable.map(({ outcome, count }) => (
                        <span key={outcome} className="inline-flex items-center gap-1">
                          <OutcomeIcon outcome={outcome} />
                          {count} {outcomeLabel(outcome, t)}
                        </span>
                      ))}
                    </p>
                  )}
                </div>
                <span className="ml-auto shrink-0 pl-2 text-micro tabular-nums text-muted-foreground">
                  {t(($) => $.app_calls.calls_count, { count: group.calls.length })}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
