"use client";

import { useMemo } from "react";
import { ChevronRight, History } from "lucide-react";
import { useQueries } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import {
  projectReleasesOptions,
  useCollapsedProjects,
  useShipProjects,
} from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import type { Release } from "@multica/core/types";
import { formatDeployedAt, releaseStageColorClass } from "./release-utils";

const TERMINAL_STAGES = new Set(["done", "cancelled", "rolled_back"]);
const HISTORY_LIMIT = 10;

function releaseTerminalAt(r: Release): string {
  return r.done_at ?? r.promoted_at ?? r.updated_at;
}

export function ShipReleaseHistory() {
  const { t } = useT("ship");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const slug = workspace?.slug ?? "";

  const { data: projectsData } = useShipProjects(true);
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  const projectTitleById = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of projects) m.set(p.id, p.title);
    return m;
  }, [projects]);

  const queries = useQueries({
    queries: projects.map((p) => projectReleasesOptions(wsId, p.id, "all")),
  });

  const isLoading = queries.some((q) => q.isLoading);

  const historyReleases = useMemo(() => {
    const result: Release[] = [];
    for (const q of queries) {
      for (const r of q.data?.releases ?? []) {
        if (TERMINAL_STAGES.has(r.stage)) result.push(r);
      }
    }
    return result
      .sort(
        (a, b) =>
          new Date(releaseTerminalAt(b)).getTime() -
          new Date(releaseTerminalAt(a)).getTime(),
      )
      .slice(0, HISTORY_LIMIT);
  }, [queries]);

  const collapsed = useCollapsedProjects((s) => s.releaseHistoryCollapsed);
  const toggle = useCollapsedProjects((s) => s.toggleReleaseHistory);

  if (!isLoading && historyReleases.length === 0) return null;

  return (
    <section
      className="rounded-md border bg-card p-3"
      data-testid="ship-release-history"
    >
      <header className="mb-2 flex items-center gap-2 text-sm font-medium">
        <button
          type="button"
          onClick={toggle}
          className="flex size-6 shrink-0 items-center justify-center rounded hover:bg-muted"
          aria-expanded={!collapsed}
          aria-controls="ship-release-history-content"
          aria-label={
            collapsed ? "Expand release history" : "Collapse release history"
          }
          data-testid="ship-release-history-toggle"
        >
          <ChevronRight
            className={cn(
              "size-4 text-muted-foreground transition-transform",
              !collapsed && "rotate-90",
            )}
          />
        </button>
        <History className="size-4 text-muted-foreground" aria-hidden />
        {t(($) => $.releases.history_title)}
      </header>

      {!collapsed && (
        <div id="ship-release-history-content">
          {isLoading && historyReleases.length === 0 ? (
            <p className="text-xs text-muted-foreground">…</p>
          ) : (
            <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {historyReleases.map((release) => (
                <li key={release.id} data-testid="ship-history-release-card">
                  {slug ? (
                    <AppLink
                      href={`/${slug}/ship/release/${release.id}`}
                      className="block rounded border bg-background p-2.5 text-sm transition-colors hover:bg-muted/50"
                      data-testid="ship-history-release-view"
                    >
                      <div className="flex items-center gap-2">
                        <span
                          className="truncate font-medium"
                          title={release.title}
                        >
                          {release.title}
                        </span>
                        <span
                          className={cn(
                            "ml-auto rounded px-1 py-0.5 text-[10px] uppercase tracking-wide",
                            releaseStageColorClass(release.stage),
                          )}
                        >
                          {t(
                            ($) =>
                              ($.releases.stage as Record<string, string>)[
                                release.stage
                              ] ?? $.releases.stage.assembling,
                          )}
                        </span>
                      </div>
                      {projectTitleById.get(release.project_id) && (
                        <span className="block truncate text-xs text-muted-foreground">
                          {projectTitleById.get(release.project_id)}
                        </span>
                      )}
                      <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                        <span>
                          {release.pr_count} PR
                          {release.pr_count === 1 ? "" : "s"}
                        </span>
                      </div>
                      <time
                        dateTime={releaseTerminalAt(release)}
                        className="mt-0.5 block text-xs text-muted-foreground"
                      >
                        {formatDeployedAt(releaseTerminalAt(release))}
                      </time>
                    </AppLink>
                  ) : (
                    <div className="rounded border bg-background p-2.5 text-sm">
                      <div className="flex items-center gap-2">
                        <span
                          className="truncate font-medium"
                          title={release.title}
                        >
                          {release.title}
                        </span>
                        <span
                          className={cn(
                            "ml-auto rounded px-1 py-0.5 text-[10px] uppercase tracking-wide",
                            releaseStageColorClass(release.stage),
                          )}
                        >
                          {t(
                            ($) =>
                              ($.releases.stage as Record<string, string>)[
                                release.stage
                              ] ?? $.releases.stage.assembling,
                          )}
                        </span>
                      </div>
                      {projectTitleById.get(release.project_id) && (
                        <span className="block truncate text-xs text-muted-foreground">
                          {projectTitleById.get(release.project_id)}
                        </span>
                      )}
                      <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                        <span>
                          {release.pr_count} PR
                          {release.pr_count === 1 ? "" : "s"}
                        </span>
                      </div>
                      <time
                        dateTime={releaseTerminalAt(release)}
                        className="mt-0.5 block text-xs text-muted-foreground"
                      >
                        {formatDeployedAt(releaseTerminalAt(release))}
                      </time>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}
