"use client";

import { CheckCircle2, XCircle, Clock, GitPullRequest, AlertTriangle } from "lucide-react";
import type { PullRequest } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { deriveRiskHint } from "../hooks/use-pr-state";

interface ShipPRCardProps {
  pr: PullRequest;
}

function formatRelativeTime(iso: string, locale: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "";
  const diffMs = Date.now() - then;
  const diffSec = Math.max(1, Math.round(diffMs / 1000));
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  // Pick the largest sensible unit so we don't produce "120 minutes ago"
  // when "2 hours ago" reads better. Negative because time has passed.
  const buckets: [number, Intl.RelativeTimeFormatUnit][] = [
    [60, "second"],
    [60, "minute"],
    [24, "hour"],
    [7, "day"],
    [4.345, "week"],
    [12, "month"],
    [Number.POSITIVE_INFINITY, "year"],
  ];
  let value = diffSec;
  for (const [div, unit] of buckets) {
    if (value < div) {
      return rtf.format(-Math.round(value), unit);
    }
    value /= div;
  }
  return rtf.format(-Math.round(value), "year");
}

/** CI status pill. Returns one of three states; renders nothing for blank
 *  ci_status (server hasn't seen a check run yet) so the card stays clean. */
function CIPill({ status }: { status: string }) {
  const { t, i18n } = useT("ship");
  void i18n; // keep import tree-shakeable
  if (status === "success") {
    return (
      <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="size-3" />
        {t(($) => $.card.ci_passing)}
      </span>
    );
  }
  if (status === "failure") {
    return (
      <span className="inline-flex items-center gap-1 text-destructive">
        <XCircle className="size-3" />
        {t(($) => $.card.ci_failing)}
      </span>
    );
  }
  if (status === "pending") {
    return (
      <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
        <Clock className="size-3" />
        {t(($) => $.card.ci_pending)}
      </span>
    );
  }
  return null;
}

export function ShipPRCard({ pr }: ShipPRCardProps) {
  const { t, i18n } = useT("ship");
  const risk = deriveRiskHint(pr);

  return (
    <a
      href={pr.html_url}
      target="_blank"
      rel="noopener noreferrer"
      // Use semantic tokens for the card surface — explicit hover lift to
      // signal it's clickable. Phase 1 has no inline preview; click goes
      // straight to GitHub in a new tab.
      className={cn(
        "block rounded-md border bg-card p-3 text-card-foreground shadow-sm",
        "transition-colors hover:border-primary/40 hover:bg-accent/40",
        pr.is_draft && "opacity-80",
      )}
    >
      {/* Author + PR number row. Avatar is just the GitHub URL — no
          additional preflight; the user already trusts the destination. */}
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        {pr.author_avatar_url ? (
          <img
            src={pr.author_avatar_url}
            alt=""
            aria-hidden
            className="size-4 rounded-full"
          />
        ) : (
          <GitPullRequest className="size-3.5" />
        )}
        <span className="truncate">{pr.author_login}</span>
        <span aria-hidden>·</span>
        <span className="tabular-nums">#{pr.number}</span>
        {pr.is_draft && (
          <span className="ml-auto rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide">
            {t(($) => $.card.draft_pill)}
          </span>
        )}
      </div>

      {/* Title — single line truncate so the card height stays predictable
          and the Kanban columns visually align. The full title is in the
          tooltip via the native title attribute. */}
      <div
        title={pr.title}
        className="mt-1 truncate text-sm font-medium text-foreground"
      >
        {pr.title}
      </div>

      {/* Diff stats — show only when at least one is non-zero. A brand-new
          PR with no diff loaded still has zeroes from the server, no point
          showing "+0 -0". */}
      {(pr.additions > 0 || pr.deletions > 0 || pr.changed_files > 0) && (
        <div className="mt-1.5 flex items-center gap-2 text-xs text-muted-foreground tabular-nums">
          <span>
            {t(($) => $.card.additions_deletions, {
              additions: pr.additions,
              deletions: pr.deletions,
            })}
          </span>
          <span aria-hidden>·</span>
          <span>
            {t(($) => $.card.files_count, { count: pr.changed_files })}
          </span>
        </div>
      )}

      {/* CI / mergeable pill row. Hidden when nothing to say. */}
      {(pr.ci_status || pr.mergeable === "CONFLICTING") && (
        <div className="mt-1.5 flex items-center gap-2 text-xs">
          <CIPill status={pr.ci_status} />
          {pr.mergeable === "CONFLICTING" && (
            <span className="inline-flex items-center gap-1 text-destructive">
              <AlertTriangle className="size-3" />
              {t(($) => $.kanban.conflicting)}
            </span>
          )}
        </div>
      )}

      {/* Risk hint — phase 1 keyword detection. The chip shows up only for
          true matches so it stays meaningful. */}
      {risk && (
        <div className="mt-1.5 inline-flex items-center gap-1 rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-400">
          <AlertTriangle className="size-3" />
          {risk === "schema"
            ? t(($) => $.card.risk_schema)
            : t(($) => $.card.risk_migration)}
        </div>
      )}

      <div className="mt-1.5 text-[11px] text-muted-foreground">
        {t(($) => $.card.updated_relative, {
          when: formatRelativeTime(pr.pr_updated_at, i18n.language),
        })}
      </div>
    </a>
  );
}
