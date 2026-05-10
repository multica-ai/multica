"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw, AlertCircle, ChevronRight } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useProjectPullRequests, useSyncProject } from "@multica/core/ship";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { PullRequest, ShipProjectSummary } from "@multica/core/types";
import { projectListOptions } from "@multica/core/projects/queries";
import { AppLink } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { ShipKanban } from "./ship-kanban";
import { ShipDeploySwimlanes } from "./ship-deploy-swimlanes";

interface ShipProjectSectionProps {
  project: ShipProjectSummary;
}

/**
 * Pull a Project (with full type) from the cached project list so we can
 * pass it to ProjectIcon — the ship endpoint returns a slimmer summary
 * that doesn't include `archived_at` etc., and ProjectIcon expects the
 * full Project type.
 */
function useFullProject(projectId: string) {
  const wsId = useWorkspaceId();
  const { data } = useQuery(projectListOptions(wsId));
  return useMemo(
    () => (data ?? []).find((p) => p.id === projectId) ?? null,
    [data, projectId],
  );
}

export function ShipProjectSection({ project }: ShipProjectSectionProps) {
  const { t } = useT("ship");
  const wsPaths = useWorkspacePaths();
  const fullProject = useFullProject(project.id);

  // Phase 1 fetches "all" PRs and buckets locally — it lets the Kanban
  // surface "Recently merged" without a second round-trip. The result
  // sets are tiny (typically <50 PRs per project), so a single fetch is
  // the right tradeoff vs. 4 state-filtered queries.
  const { data, isLoading, error, isFetching } = useProjectPullRequests(
    project.id,
    "all",
  );
  const sync = useSyncProject();

  const prs: PullRequest[] = useMemo(
    () => data?.pull_requests ?? [],
    [data],
  );

  const [errorBanner, setErrorBanner] = useState<{
    title: string;
    description: string;
  } | null>(null);

  const handleSync = async () => {
    setErrorBanner(null);
    try {
      await sync.mutateAsync(project.id);
    } catch (e) {
      // Translate typed API errors into the spec'd error states.
      if (e instanceof ApiError) {
        if (e.status === 401) {
          setErrorBanner({
            title: t(($) => $.errors.token_expired_title),
            description: t(($) => $.errors.token_expired_description),
          });
          return;
        }
        if (e.status === 429) {
          setErrorBanner({
            title: t(($) => $.errors.rate_limited_title),
            description: t(($) => $.errors.rate_limited_description),
          });
          return;
        }
        setErrorBanner({
          title: t(($) => $.errors.sync_failed_title),
          description: t(($) => $.errors.sync_failed_description, {
            message: e.message,
          }),
        });
        return;
      }
      setErrorBanner({
        title: t(($) => $.errors.sync_failed_title),
        description: t(($) => $.errors.sync_failed_description, {
          message: e instanceof Error ? e.message : String(e),
        }),
      });
    }
  };

  // The list endpoint also fails 401/429 on the auto-fetch; surface those
  // up front so the user understands why the column is empty.
  const fetchErrorBanner = useMemo(() => {
    if (!error || !(error instanceof ApiError)) return null;
    if (error.status === 401) {
      return {
        title: t(($) => $.errors.token_expired_title),
        description: t(($) => $.errors.token_expired_description),
      };
    }
    if (error.status === 429) {
      return {
        title: t(($) => $.errors.rate_limited_title),
        description: t(($) => $.errors.rate_limited_description),
      };
    }
    return null;
  }, [error, t]);

  const banner = errorBanner ?? fetchErrorBanner;

  // Phase 7e — collapsible per-project sections. With several projects on
  // the Ship Hub page, the user often only cares about one or two; the
  // others can be folded so the relevant Kanban + deploy lanes stay
  // above the fold. State is intentionally local + ephemeral (CLAUDE.md
  // "Don't persist ephemeral UI state" — collapse on a fresh page load
  // is fine; the in-memory store survives sync refetches but not
  // navigation).
  const [collapsed, setCollapsed] = useState(false);

  // Summary counts shown in the header (always visible, but most useful
  // when collapsed — they tell the user "is there anything to act on
  // here without expanding?"). Derived from the full PR list rather
  // than `project.open_pr_count` so the categories stay consistent
  // (open_pr_count is from the list endpoint and doesn't break out
  // drafts vs blocked).
  const summary = useMemo(() => {
    const openOnly = prs.filter((pr) => pr.state === "open");
    const draft = openOnly.filter((pr) => pr.is_draft).length;
    const blocked = openOnly.filter(
      (pr) => pr.ci_status === "failure" || pr.mergeable === "CONFLICTING",
    ).length;
    const readyToMerge = openOnly.filter(
      (pr) =>
        !pr.is_draft &&
        pr.review_decision === "APPROVED" &&
        pr.ci_status === "success" &&
        pr.mergeable !== "CONFLICTING",
    ).length;
    return { open: openOnly.length, draft, blocked, readyToMerge };
  }, [prs]);

  const sectionId = `ship-project-${project.id}`;

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          className="flex size-6 shrink-0 items-center justify-center rounded hover:bg-muted"
          aria-expanded={!collapsed}
          aria-controls={sectionId}
          aria-label={collapsed ? "Expand project" : "Collapse project"}
          data-testid="ship-project-toggle"
        >
          <ChevronRight
            className={cn(
              "size-4 text-muted-foreground transition-transform",
              !collapsed && "rotate-90",
            )}
          />
        </button>
        <AppLink
          href={wsPaths.projectDetail(project.id)}
          className="flex min-w-0 items-center gap-2"
        >
          {fullProject ? (
            <ProjectIcon project={fullProject} size="md" />
          ) : (
            <span className="inline-block size-4 rounded bg-muted" aria-hidden />
          )}
          <h2 className="truncate text-base font-semibold">{project.title}</h2>
        </AppLink>
        {/* Summary chips — always visible. Acts as both "what's in here"
            (when collapsed) and a quick-status header (when expanded).
            The single `n` chip preserves Phase 1's open-count UI; the
            extra chips surface only when non-zero so the header stays
            quiet for projects with nothing notable. */}
        <span
          className="text-xs text-muted-foreground tabular-nums"
          title={`${summary.open} open`}
        >
          {summary.open}
        </span>
        {summary.readyToMerge > 0 && (
          <span
            className="rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 tabular-nums dark:text-emerald-300"
            title={`${summary.readyToMerge} ready to merge`}
          >
            {summary.readyToMerge} ready
          </span>
        )}
        {summary.blocked > 0 && (
          <span
            className="rounded border border-destructive/40 bg-destructive/10 px-1.5 py-0.5 text-[10px] font-medium text-destructive tabular-nums"
            title={`${summary.blocked} blocked (failing CI or conflicts)`}
          >
            {summary.blocked} blocked
          </span>
        )}
        {summary.draft > 0 && (
          <span
            className="rounded border border-muted-foreground/30 bg-muted/30 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground tabular-nums"
            title={`${summary.draft} draft`}
          >
            {summary.draft} draft
          </span>
        )}
        <div className="ml-auto">
          <Button
            size="sm"
            variant="outline"
            onClick={handleSync}
            disabled={sync.isPending}
          >
            <RefreshCw
              className={`size-3 ${sync.isPending || isFetching ? "animate-spin" : ""}`}
            />
            {sync.isPending
              ? t(($) => $.page.syncing)
              : t(($) => $.page.sync_all)}
          </Button>
        </div>
      </div>

      {banner && (
        <div
          className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3"
          role="alert"
        >
          <AlertCircle className="size-4 shrink-0 text-destructive" />
          <div className="min-w-0">
            <p className="text-sm font-medium text-destructive">{banner.title}</p>
            <p className="text-xs text-muted-foreground">{banner.description}</p>
          </div>
        </div>
      )}

      {!collapsed && (
        <div id={sectionId} className="space-y-3">
          <ShipDeploySwimlanes projectId={project.id} />

          <ShipKanban
            pullRequests={prs}
            isLoading={isLoading}
            projectId={project.id}
          />
        </div>
      )}
    </section>
  );
}
