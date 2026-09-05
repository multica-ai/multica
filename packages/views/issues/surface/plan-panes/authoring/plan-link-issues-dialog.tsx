"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Link2, Loader2, X } from "lucide-react";
import { api } from "@multica/core/api";
import { issueStatusCategory } from "@multica/core/issues";
import type { ProjectPlanPart } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../../../i18n";
import { StatusIcon } from "../../../components/status-icon";
import { PlanWriteNotice } from "./plan-write-notice";
import { usePlanWriteError } from "./use-plan-write-error";

/**
 * Link and unlink the issues that cover one plan part.
 *
 * Search is scoped to the plan's own project through `project_id`, because
 * `Service.LinkIssue` resolves the issue with
 * `GetProjectPlanSourceIssueForWrite(id, workspace, plan.ProjectID)` and 404s
 * anything outside it. Offering a workspace-wide picker would mean offering
 * choices the server will reject.
 *
 * `linkedElsewhere` is the same rule one level up. The uniqueness constraint
 * behind `ErrorIssueAlreadyLinked` is
 * `project_plan_part_issue_plan_issue_key` — (plan, issue), NOT (part, issue)
 * — so an issue linked to ANY part of this plan cannot be linked to a second
 * one. Filtering only this part's own issues left the other parts' issues in
 * the list, every one of them a guaranteed 409.
 *
 * The linked list comes from `part.issues` — the server-computed read model —
 * so this dialog never asserts a link the API has not confirmed. After a write
 * it closes the row and the pane re-reads; it does not patch the list itself.
 */
export function PlanLinkIssuesDialog({
  open,
  onOpenChange,
  projectId,
  part,
  linkedElsewhere,
  onLink,
  onUnlink,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId: string;
  part: ProjectPlanPart;
  /** Issue ids already linked to any part of this plan, this one included. */
  linkedElsewhere: ReadonlySet<string>;
  onLink: (issueId: string) => Promise<void>;
  onUnlink: (issueId: string) => Promise<void>;
}) {
  const { t } = useT("issues");
  const { notice, report, clear } = usePlanWriteError();
  const [query, setQuery] = useState("");
  const [pendingId, setPendingId] = useState<string | null>(null);

  const linkedIds = useMemo(
    () => new Set(part.issues.map((issue) => issue.id).filter((id): id is string => !!id)),
    [part.issues],
  );

  // `isError` is read, not defaulted away. With only `data = []` a rejected
  // request left the list empty and the reader was told "no issues match" when
  // the truth was that the search never ran — the silent fallback AC 7
  // forbids. The empty message below is now reserved for a query that
  // succeeded and genuinely returned nothing.
  const {
    data: candidates,
    isError,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ["project-plan-link-candidates", projectId, query.trim()],
    queryFn: () =>
      api
        .listIssues({ project_id: projectId, q: query.trim() || undefined, limit: 25 })
        .then((res) => res.issues),
    enabled: open,
    staleTime: 10_000,
    retry: false,
  });

  const selectable = (candidates ?? []).filter(
    (issue) => !linkedIds.has(issue.id) && !linkedElsewhere.has(issue.id),
  );

  const run = async (issueId: string, action: (id: string) => Promise<void>) => {
    setPendingId(issueId);
    clear();
    try {
      await action(issueId);
    } catch (err) {
      report(err);
    } finally {
      setPendingId(null);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (pendingId) return;
        onOpenChange(next);
      }}
    >
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-[560px]">
        <DialogHeader>
          <DialogTitle className="text-title-sm font-semibold">
            {t(($) => $.plan.authoring.link_issues.title)}
          </DialogTitle>
          <DialogDescription className="text-body text-pretty">
            {t(($) => $.plan.authoring.link_issues.description, { part: part.title })}
          </DialogDescription>
        </DialogHeader>

        <div className="min-w-0 flex flex-col gap-3">
          <section className="flex flex-col gap-1.5">
            <h3 className="text-caption font-medium text-muted-foreground">
              {t(($) => $.plan.authoring.link_issues.linked_heading)}
            </h3>
            {part.issues.length === 0 ? (
              // The no-tasks-yet state, stated plainly. Not "0%" — a part with
              // no linked issues has no progress to report, and inventing one
              // is the failure this whole feature exists to prevent.
              <p className="rounded-md border border-dashed border-warning/40 bg-warning/5 px-3 py-2 text-caption text-muted-foreground">
                {t(($) => $.plan.coverage_hint.no_tasks_yet)}
              </p>
            ) : (
              <ul className="flex flex-col gap-1">
                {part.issues.map((issue) => (
                  <li
                    key={issue.identifier}
                    className="flex items-center gap-2 rounded-md border border-surface-border bg-surface px-2.5 py-1.5"
                  >
                    <span className="w-16 shrink-0 text-micro text-muted-foreground tabular-nums">
                      {issue.identifier}
                    </span>
                    <Tooltip>
                      <TooltipTrigger
                        aria-label={issue.title}
                        render={
                          <span tabIndex={0} className="min-w-0 flex-1 truncate text-caption" />
                        }
                      >
                        {issue.title}
                      </TooltipTrigger>
                      <TooltipContent className="whitespace-normal break-words">
                        {issue.title}
                      </TooltipContent>
                    </Tooltip>
                    {issue.id && !issue.deleted ? (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t(($) => $.plan.authoring.link_issues.unlink_label, {
                          identifier: issue.identifier,
                        })}
                        disabled={!!pendingId}
                        onClick={() => void run(issue.id!, onUnlink)}
                      >
                        {pendingId === issue.id ? (
                          <Loader2 className="size-3.5 animate-spin" />
                        ) : (
                          <X className="size-3.5" />
                        )}
                      </Button>
                    ) : (
                      // A deleted issue has no id to unlink by; the read model
                      // still reports the row, so say why it cannot be acted on.
                      <span className="px-2 text-micro text-muted-foreground">
                        {t(($) => $.plan.authoring.link_issues.deleted_issue)}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="flex flex-col gap-1.5">
            <h3 className="text-caption font-medium text-muted-foreground">
              {t(($) => $.plan.authoring.link_issues.search_heading)}
            </h3>
            <Input
              value={query}
              placeholder={t(($) => $.plan.authoring.link_issues.search_placeholder)}
              onChange={(e) => setQuery(e.target.value)}
            />
            <div className="max-h-[220px] overflow-y-auto rounded-md border border-surface-border">
              {isError ? (
                // Ordered before every other branch: a failed search is not an
                // empty search, and must never be reported as one.
                <div
                  role="alert"
                  data-testid="plan-link-candidates-error"
                  className="flex flex-col items-center gap-2 px-3 py-4 text-center"
                >
                  <AlertTriangle aria-hidden="true" className="size-4 text-destructive" />
                  <p className="text-caption text-foreground">
                    {t(($) => $.plan.authoring.link_issues.load_failed)}
                  </p>
                  <p className="text-micro text-muted-foreground">
                    {t(($) => $.plan.authoring.link_issues.load_failed_hint)}
                  </p>
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    onClick={() => void refetch()}
                    disabled={isFetching}
                  >
                    {isFetching
                      ? t(($) => $.plan.authoring.link_issues.searching)
                      : t(($) => $.plan.authoring.link_issues.retry)}
                  </Button>
                </div>
              ) : isFetching && selectable.length === 0 ? (
                <p className="px-3 py-4 text-center text-caption text-muted-foreground">
                  {t(($) => $.plan.authoring.link_issues.searching)}
                </p>
              ) : selectable.length === 0 ? (
                <p
                  data-testid="plan-link-candidates-empty"
                  className="px-3 py-4 text-center text-caption text-muted-foreground"
                >
                  {t(($) => $.plan.authoring.link_issues.no_candidates)}
                </p>
              ) : (
                <ul>
                  {selectable.map((issue) => (
                    <li key={issue.id}>
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <button
                              type="button"
                              aria-label={issue.title}
                              disabled={!!pendingId}
                              onClick={() => void run(issue.id, onLink)}
                              className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left hover:bg-surface-hover disabled:opacity-60 transition-colors"
                            >
                              <StatusIcon
                                status={issue.status}
                                category={issueStatusCategory(issue) ?? undefined}
                                className="size-3.5 shrink-0"
                              />
                              <span className="w-16 shrink-0 text-micro text-muted-foreground tabular-nums">
                                {issue.identifier}
                              </span>
                              <span className="min-w-0 flex-1 truncate text-caption">
                                {issue.title}
                              </span>
                              {pendingId === issue.id ? (
                                <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
                              ) : (
                                <Link2 className="size-3.5 shrink-0 text-faint-foreground" />
                              )}
                            </button>
                          }
                        />
                        <TooltipContent className="whitespace-normal break-words">
                          {issue.title}
                        </TooltipContent>
                      </Tooltip>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>

          <PlanWriteNotice notice={notice} />
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={!!pendingId}>
            {t(($) => $.plan.authoring.done)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
