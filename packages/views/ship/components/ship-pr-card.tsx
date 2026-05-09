"use client";

import {
  CheckCircle2,
  XCircle,
  Clock,
  GitPullRequest,
  AlertTriangle,
  HelpCircle,
} from "lucide-react";
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

/** CI status pill — Phase 2 surface. With webhooks online the value
 *  arrives in real time; we still default to silent ("") for fresh
 *  PRs that haven't had a check run yet. The `unknown` branch is
 *  reached when the server reports a status string we don't recognize
 *  (forward-compat per CLAUDE.md "API Response Compatibility"). */
function CIPill({ status }: { status: string }) {
  const { t } = useT("ship");
  if (!status) return null;
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
  // Unknown enum value — render a generic fallback so the user can still
  // see "something is reported" without crashing.
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground">
      <HelpCircle className="size-3" />
      {t(($) => $.card.ci_unknown)}
    </span>
  );
}

/** Review-decision badge. Phase 2 — backend now populates this from PR
 *  review webhooks. Empty string ("") is the "no decision yet" state and
 *  intentionally renders nothing so the card stays clean for fresh PRs.
 *
 *  The card uses the badge AS WELL AS the column placement: Ready-to-Land
 *  cards land in the green column, but the badge surfaces the same signal
 *  on the failing/blocked rail (where columns aren't visible) and on the
 *  card detail flyout. */
function ReviewBadge({ decision }: { decision: string }) {
  const { t } = useT("ship");
  if (!decision) return null;
  if (decision === "APPROVED") {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[11px] font-medium text-emerald-700 dark:text-emerald-400">
        <span className="size-1.5 rounded-full bg-emerald-500" aria-hidden />
        {t(($) => $.card.review_approved)}
      </span>
    );
  }
  if (decision === "CHANGES_REQUESTED") {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-orange-500/10 px-1.5 py-0.5 text-[11px] font-medium text-orange-700 dark:text-orange-400">
        <span className="size-1.5 rounded-full bg-orange-500" aria-hidden />
        {t(($) => $.card.review_changes_requested)}
      </span>
    );
  }
  if (decision === "REVIEW_REQUIRED") {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
        <span className="size-1.5 rounded-full bg-muted-foreground/40" aria-hidden />
        {t(($) => $.card.review_required)}
      </span>
    );
  }
  // Unknown enum — degrade silently rather than render an unstyled chip.
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

      {/* CI / review / mergeable pill row. Hidden when nothing to say.
          Render review_decision next to ci_status because they read as
          a unit ("approved + passing CI" / "changes requested + failing"
          etc.). Conflict warning is its own chip because it's a hard
          blocker independent of either signal. */}
      {(pr.ci_status || pr.review_decision || pr.mergeable === "CONFLICTING") && (
        <div className="mt-1.5 flex flex-wrap items-center gap-2 text-xs">
          <CIPill status={pr.ci_status} />
          <ReviewBadge decision={pr.review_decision} />
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
