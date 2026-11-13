"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Ban, CircleCheck, Clock, XCircle } from "@multica/ui/icons";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { appCatalogOptions, issueAppCallsOptions } from "@multica/core/connected-apps";
import type { IssueAppCall } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT } from "../../i18n";

const MAX_CHIPS = 6;

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

/** The calls of an issue's runs, and app display names. Both the comment
 * chips and the sidebar read this one query. */
export function useIssueAppCalls(issueId: string) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const calls = useQuery({ ...issueAppCallsOptions(wsId, issueId), enabled });
  const catalog = useQuery({ ...appCatalogOptions(wsId), enabled: enabled && (calls.data?.length ?? 0) > 0 });
  const appName = useMemo(() => {
    const names = new Map<string, string>();
    for (const app of catalog.data?.apps ?? []) names.set(app.slug, app.name);
    return (slug: string) => names.get(slug) || slug;
  }, [catalog.data]);
  return { calls: calls.data ?? [], appName };
}

/** `GMAIL_SEND_EMAIL` of `gmail` reads as "Send email". */
export function toolLabel(toolkitSlug: string, toolSlug: string): string {
  const prefix = `${toolkitSlug.toUpperCase().replace(/-/g, "_")}_`;
  const bare = toolSlug.toUpperCase().startsWith(prefix) ? toolSlug.slice(prefix.length) : toolSlug;
  const words = bare.toLowerCase().replace(/_+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : toolSlug;
}

export function outcomeLabel(outcome: string, t: IssuesT): string {
  switch (outcome) {
    case "ok":
    case "error":
    case "blocked":
    case "pending":
    case "approved":
    case "rejected":
    case "rate_limited":
      return t(($) => $.app_calls.outcome[outcome]);
    default:
      return outcome;
  }
}

export function OutcomeIcon({ outcome, className }: { outcome: string; className?: string }) {
  const cls = cn("size-3 shrink-0", className);
  switch (outcome) {
    case "ok":
    case "approved":
      return <CircleCheck className={cn(cls, "text-success")} />;
    case "pending":
    case "rate_limited":
      return <Clock className={cn(cls, "text-warning")} />;
    case "blocked":
      return <Ban className={cn(cls, "text-muted-foreground")} />;
    default:
      return <XCircle className={cn(cls, "text-destructive")} />;
  }
}

type ChipGroup = { key: string; toolkit: string; tool: string; calls: IssueAppCall[] };

/** What the run behind an agent comment did in apps: one chip per tool,
 * marked with how its latest call ended. */
export function AppCallChips({ issueId, taskId, className }: { issueId: string; taskId: string; className?: string }) {
  const { t } = useT("issues");
  const { calls, appName } = useIssueAppCalls(issueId);
  const groups = useMemo(() => {
    const byTool = new Map<string, ChipGroup>();
    // Calls arrive newest first, so each group's first call is its latest.
    for (const call of calls) {
      if (call.task_id !== taskId) continue;
      const key = `${call.toolkit_slug}:${call.tool_slug}`;
      const group = byTool.get(key);
      if (group) group.calls.push(call);
      else byTool.set(key, { key, toolkit: call.toolkit_slug, tool: call.tool_slug, calls: [call] });
    }
    return [...byTool.values()].reverse();
  }, [calls, taskId]);

  if (groups.length === 0) return null;
  const shown = groups.slice(0, MAX_CHIPS);
  const hidden = groups.length - shown.length;

  return (
    <div className={cn("mt-1.5 flex flex-wrap items-center gap-1", className)} aria-label={t(($) => $.app_calls.chips_label)}>
      {shown.map((group) => {
        const latest = group.calls[0]!;
        const app = appName(group.toolkit);
        return (
          <Tooltip key={group.key}>
            <TooltipTrigger
              render={
                <span className="inline-flex h-6 max-w-56 items-center gap-1.5 rounded-md border bg-card px-1.5 text-micro font-medium text-muted-foreground">
                  <ComposioToolkitLogo slug={group.toolkit} name={app} className="size-3.5 rounded-xs text-[8px]" />
                  <span className="truncate text-foreground">{toolLabel(group.toolkit, group.tool)}</span>
                  {group.calls.length > 1 && <span className="tabular-nums">×{group.calls.length}</span>}
                  <OutcomeIcon outcome={latest.outcome} />
                </span>
              }
            />
            <TooltipContent side="top" className="max-w-72 text-caption">
              <p className="font-medium">{app} · {group.tool}</p>
              <p>
                {outcomeLabel(latest.outcome, t)} · {t(($) => $.app_calls.scope[latest.account_scope])}
              </p>
              {latest.error && <p className="mt-1 line-clamp-3 opacity-80">{latest.error}</p>}
            </TooltipContent>
          </Tooltip>
        );
      })}
      {hidden > 0 && (
        <span className="inline-flex h-6 items-center px-1 text-micro font-medium text-muted-foreground">
          {t(($) => $.app_calls.more, { count: hidden })}
        </span>
      )}
    </div>
  );
}
