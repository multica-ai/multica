"use client";

// Phase 7a — Active releases rail.
//
// Renders at the top of the Ship Hub landing page above the
// per-project Kanban sections. Lists every active release in the
// workspace as a small card with title + project + stage + PR count.
// Clicking "View" navigates to the release detail page.

import { ChevronRight, Train } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { useActiveReleases, useCollapsedProjects } from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";

export function ShipActiveReleasesRail() {
  const { t } = useT("ship");
  const workspace = useCurrentWorkspace();
  const { data, isLoading } = useActiveReleases(true);
  const releases = data?.releases ?? [];
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
                <li
                  key={release.id}
                  className="rounded border bg-background p-2.5 text-sm"
                  data-testid="ship-active-release-card"
                >
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium" title={release.title}>
                      {release.title}
                    </span>
                    <span className="ml-auto text-[10px] uppercase tracking-wide text-muted-foreground">
                      {t(
                        ($) =>
                          $.releases.stage[
                            release.stage as keyof typeof $.releases.stage
                          ] ?? $.releases.stage.assembling,
                      )}
                    </span>
                  </div>
                  <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                    <span>
                      {release.pr_count} PR{release.pr_count === 1 ? "" : "s"}
                    </span>
                    <span aria-hidden>·</span>
                    <span className="capitalize">{release.risk_level}</span>
                  </div>
                  {slug && (
                    <AppLink
                      href={`/${slug}/ship/release/${release.id}`}
                      className="mt-2 inline-block text-xs text-primary hover:underline"
                      data-testid="ship-active-release-view"
                    >
                      {t(($) => $.releases.view_release)} →
                    </AppLink>
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
