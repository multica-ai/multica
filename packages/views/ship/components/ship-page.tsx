"use client";

import { useEffect, useMemo } from "react";
import { Rocket } from "lucide-react";
import { useShipProjects, useShipSelection } from "@multica/core/ship";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useQueries } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectPullRequestsOptions } from "@multica/core/ship";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { ShipEmptyState } from "./ship-empty-state";
import { ShipNoTokenState } from "./ship-no-token-state";
import { ShipProjectSection } from "./ship-project-section";
import { ShipActiveReleasesRail } from "./ship-active-releases-rail";
import { ShipSelectionBar } from "./ship-selection-bar";
import { ShipPrDetailDrawer } from "./ship-pr-detail-drawer";
import type { PullRequest } from "@multica/core/types";

/**
 * Ship Hub landing page.
 *
 * Surface gating:
 *   1. Workspace flag off → polite "feature is off" message.
 *      (Not a 404 because the user might land here from a stale link.)
 *   2. Workspace flag on, GitHub token NOT configured → no-token state.
 *      The list endpoint returns 200/empty in this case but a sync would
 *      400; we steer the admin to settings up front.
 *   3. Flag + token but no projects with attached repos → empty state.
 *   4. Flag + token + ≥1 project → render the per-project Kanban + deploy
 *      strip stack, sorted by project order from the API.
 *
 * The page intentionally renders one big scroll surface rather than a
 * tabbed/per-project view. Phase-1 teams have a handful of projects;
 * stacking lets the user sweep the whole workspace at once. Phase 5
 * revisits this for larger orgs.
 */
export function ShipPage() {
  const { t } = useT("ship");
  const workspace = useCurrentWorkspace();
  const enabled = workspace?.ship_hub_enabled === true;
  const tokenSet = workspace?.github_token_set === true;
  const { data, isLoading } = useShipProjects(enabled);
  const clearSelection = useShipSelection((s) => s.clear);

  // Phase 7a — clear the multi-select state on workspace switch.
  // Selection is in-memory ephemeral state (per CLAUDE.md "Don't
  // persist ephemeral UI state"); switching workspaces should not
  // bring stale PR ids forward.
  const wsId = useWorkspaceId();
  useEffect(() => {
    return () => {
      clearSelection();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key is wsId
  }, [wsId]);

  if (!enabled) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader>
          <Rocket className="size-4 text-muted-foreground" />
          <h1 className="ml-2 text-sm font-medium">{t(($) => $.page.title)}</h1>
        </PageHeader>
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center">
          <h2 className="text-lg font-semibold text-foreground">
            {t(($) => $.page.disabled_title)}
          </h2>
          <p className="max-w-md text-sm text-muted-foreground">
            {t(($) => $.page.disabled_description)}
          </p>
        </div>
      </div>
    );
  }

  const projects = data?.projects ?? [];

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="px-5">
        <Rocket className="size-4 text-muted-foreground" />
        <h1 className="ml-2 text-sm font-medium">{t(($) => $.page.title)}</h1>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="space-y-4 p-5">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="h-32 w-full" />
            <Skeleton className="h-48 w-full" />
          </div>
        ) : !tokenSet ? (
          <ShipNoTokenState />
        ) : projects.length === 0 ? (
          <ShipEmptyState />
        ) : (
          <div className="space-y-6 p-5">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.page.subtitle)}
            </p>
            {/* Phase 7a — active releases rail above the per-project
                Kanban sections. Hidden when no active releases exist. */}
            <ShipActiveReleasesRail />
            <div className="space-y-8">
              {projects.map((project) => (
                <ShipProjectSection key={project.id} project={project} />
              ))}
            </div>
          </div>
        )}
      </div>
      {/* Phase 7a — selection footer. Self-hides when nothing is
          selected. Lives outside the scrollable region so it stays
          visible while the user scrolls the project sections.
          Only mounted when the project-render branch is active —
          the consumer calls useQueries which is a no-op when
          there are zero projects, but mounting it pre-projects
          would still require a QueryClient and the no-token /
          empty / disabled branches don't need the bar. */}
      {tokenSet && projects.length > 0 && <ShipSelectionBarConsumer projects={projects} />}
      {/* PR detail drawer — mounted once at the page root so any card
          on the Kanban (or any future PR list on this page) can open
          it via the shared `useShipPrDetailStore.open(prId)`. The
          drawer is invisible when no PR is selected. */}
      <ShipPrDetailDrawer />
    </div>
  );
}

/** Consumer of every visible PR list across the workspace. The
 *  selection bar needs the union of PRs from every project section
 *  so it can derive `projectCount` + `highestRisk` without each
 *  section pushing into a side store.
 *
 *  Uses TanStack `useQueries` (plural) so the number of hook calls
 *  is constant from React's perspective — `useQueries` itself is one
 *  hook, with a variable-length array of inner queries. Calling
 *  `useQuery` inside a .map() is a rules-of-hooks violation:
 *  on the first render `projects` is empty (0 hooks), on the next
 *  render N hooks fire, and React's hook dispatcher corrupts —
 *  surfaces as a cryptic "Cannot read properties of undefined
 *  (reading 'length')" error inside an internal useEffect. */
function ShipSelectionBarConsumer({
  projects,
}: {
  projects: { id: string }[];
}) {
  const wsId = useWorkspaceId();
  // Phase 7e fix — query "all" PR states (not just open). The Kanban
  // shows PRs in MERGED · PRE-STAGING / IN STAGING / PROMOTING /
  // IN PRODUCTION columns alongside open ones, and the user can
  // multi-select any of them to create a "tracking-only" release
  // for already-merged PRs that need to be shipped together.
  // Querying just "open" meant selected merged-PR ids didn't match
  // the visible-PR set, projectCount resolved to 0, and the
  // "Create release" button stayed disabled with no obvious cause.
  const queries = useQueries({
    queries: projects.map((p) => projectPullRequestsOptions(wsId, p.id, "all")),
  });
  const allPRs = useMemo<PullRequest[]>(() => {
    const out: PullRequest[] = [];
    for (const q of queries) {
      if (q.data?.pull_requests) out.push(...q.data.pull_requests);
    }
    return out;
  }, [queries]);
  return <ShipSelectionBar pullRequests={allPRs} />;
}
