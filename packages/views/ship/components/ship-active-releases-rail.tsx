"use client";

// Phase 7a — Active releases rail.
//
// Renders at the top of the Ship Hub landing page above the
// per-project Kanban sections. Lists every active release in the
// workspace as a small card with title + project + stage + PR count.
// Clicking anywhere on the card navigates to the release detail page.

import { ChevronRight, Train } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useActiveReleases, useCollapsedProjects } from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import type { Release } from "@multica/core/types";

/** Returns the most significant deployment timestamp for a release:
 *  production deploy (promoted_at) > staging deploy (staged_at) > creation. */
function releaseDeployedAt(r: Pick<Release, "promoted_at" | "staged_at" | "created_at">): string {
  return r.promoted_at ?? r.staged_at ?? r.created_at;
}

/** Compact absolute timestamp: "May 9, 3:42 PM". Drops the year for the
 *  current year since active releases are always recent. */
function formatDeployedAt(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: d.getFullYear() !== new Date().getFullYear() ? "numeric" : undefined,
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Returns Tailwind classes for the stage badge background + text.
 *  Mirrors the progress-bar icon colors on the release detail page. */
function releaseStageColorClass(stage: string): string {
  switch (stage) {
    case "merging":
      return "bg-amber-500/20 text-amber-700 dark:text-amber-400";
    case "in_staging":
      return "bg-blue-500/20 text-blue-700 dark:text-blue-400";
    case "verifying":
      return "bg-purple-500/20 text-purple-700 dark:text-purple-400";
    case "promoting":
      return "bg-orange-500/20 text-orange-700 dark:text-orange-400";
    case "in_production":
      return "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400";
    case "done":
      return "bg-emerald-500/20 text-emerald-700 dark:text-emerald-400";
    case "rolled_back":
      return "bg-destructive/20 text-destructive";
    default:
      return "bg-muted text-muted-foreground";
  }
}

export function ShipActiveReleasesRail() {
  const { t } = useT("ship");
  const workspace = useCurrentWorkspace();
  const { data, isLoading } = useActiveReleases(true);
  const rawReleases = data?.releases ?? [];
  // Sort newest-deployed first so the most recently promoted/staged release
  // appears at the top. Mirrors the backend ORDER BY but guards against any
  // cache entries delivered in an older order.
  const releases = [...rawReleases].sort(
    (a, b) =>
      new Date(releaseDeployedAt(b)).getTime() -
      new Date(releaseDeployedAt(a)).getTime(),
  );
  const collapsed = useCollapsedProjects((s) => s.activeReleasesCollapsed);
  const toggleActiveReleases = useCollapsedProjects(
    (s) => s.toggleActiveReleases,
  );

  // Don't render the rail at all when nothing is loading and the
  // list is empty — the page-level empty state covers that case.
  // (We DO render an empty rail during initial load so the page
  // doesn't jump as the data arrives.)
  if (!isLoading && releases.length === 0) {
    return null;
  }

  const slug = workspace?.slug ?? "";

  return (
    <section
      className="rounded-md border bg-card p-3"
      data-testid="ship-active-releases-rail"
    >
      <header className="mb-2 flex items-center gap-2 text-sm font-medium">
        <button
          type="button"
          onClick={toggleActiveReleases}
          className="flex size-6 shrink-0 items-center justify-center rounded hover:bg-muted"
          aria-expanded={!collapsed}
          aria-controls="ship-active-releases-content"
          aria-label={
            collapsed ? "Expand active releases" : "Collapse active releases"
          }
          data-testid="ship-active-releases-toggle"
        >
          <ChevronRight
            className={cn(
              "size-4 text-muted-foreground transition-transform",
              !collapsed && "rotate-90",
            )}
          />
        </button>
        <Train className="size-4 text-primary" aria-hidden />
        {t(($) => $.releases.page_title)}
      </header>

      {!collapsed && (
        <div id="ship-active-releases-content">
          {isLoading && releases.length === 0 ? (
            <p className="text-xs text-muted-foreground">…</p>
          ) : (
            <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {releases.map((release) => (
                <li key={release.id} data-testid="ship-active-release-card">
                  {slug ? (
                    <AppLink
                      href={`/${slug}/ship/release/${release.id}`}
                      className="block rounded border bg-background p-2.5 text-sm transition-colors hover:bg-muted/50"
                      data-testid="ship-active-release-view"
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
                      <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                        <span>
                          {release.pr_count} PR
                          {release.pr_count === 1 ? "" : "s"}
                        </span>
                        <span aria-hidden>·</span>
                        <span className="capitalize">{release.risk_level}</span>
                      </div>
                      <time
                        dateTime={releaseDeployedAt(release)}
                        className="mt-0.5 block text-xs text-muted-foreground"
                      >
                        {formatDeployedAt(releaseDeployedAt(release))}
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
                      <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                        <span>
                          {release.pr_count} PR
                          {release.pr_count === 1 ? "" : "s"}
                        </span>
                        <span aria-hidden>·</span>
                        <span className="capitalize">{release.risk_level}</span>
                      </div>
                      <time
                        dateTime={releaseDeployedAt(release)}
                        className="mt-0.5 block text-xs text-muted-foreground"
                      >
                        {formatDeployedAt(releaseDeployedAt(release))}
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
