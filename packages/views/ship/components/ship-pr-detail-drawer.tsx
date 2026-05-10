"use client";

// PR detail drawer — rich in-app PR view that opens on card click and
// keeps the user on the Ship Hub page. Bundled into one round-trip via
// /api/pull_requests/{id}/details.
//
// Why a Sheet (right-anchored) and not a modal:
//   - The Kanban needs to remain visible behind the drawer so the user
//     can switch between cards without losing the board.
//   - Modals push the entire surface out of view; sheets land beside
//     the existing scrolling region.
//   - Mobile collapses to a full-width overlay automatically via the
//     Sheet primitive.
//
// Sections render conditionally per CLAUDE.md API drift rules — every
// optional sub-shape is null-safe, the schema parser fills empty
// arrays, and a missing field hides its section rather than rendering
// "undefined" or crashing.

import {
  CheckCircle2,
  Clock,
  ExternalLink,
  GitPullRequest,
  HelpCircle,
  Hash,
  MessagesSquare,
  Bot,
  Train,
  XCircle,
  CircleDashed,
} from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { Badge } from "@multica/ui/components/ui/badge";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  usePullRequestDetails,
  useShipPrDetailOpenId,
  useShipPrDetailStore,
  useShipSelection,
} from "@multica/core/ship";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useCurrentWorkspace } from "@multica/core/paths";
import type {
  DrawerCheck,
  DrawerReview,
  DrawerCardAction,
  DrawerPullRequestRef,
  PullRequest,
} from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";

// formatRelativeTime mirrors the helper on ship-pr-card so timestamps
// across both surfaces read the same. Pulled in-module rather than
// hoisted into a util because the card and drawer are the only callers
// today; if a third surface needs it we'll lift then.
function formatRelativeTime(iso: string | null | undefined, locale: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "";
  const diffMs = Date.now() - then;
  const diffSec = Math.max(1, Math.round(diffMs / 1000));
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
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

/** CheckIcon — iconography per check state. Mirrors the CIPill on the
 *  card but sized for the drawer (slightly larger, no label). Unknown
 *  states fall through to the help-circle icon per CLAUDE.md "Enum
 *  drift downgrades, not crashes." */
function CheckIcon({ check }: { check: DrawerCheck }) {
  const conclusion = check.conclusion ?? "";
  if (conclusion === "success") {
    return <CheckCircle2 className="size-4 text-emerald-600 dark:text-emerald-400" aria-hidden />;
  }
  if (conclusion === "failure" || conclusion === "timed_out" || conclusion === "action_required") {
    return <XCircle className="size-4 text-destructive" aria-hidden />;
  }
  if (check.status === "in_progress" || check.status === "queued") {
    return <Clock className="size-4 text-amber-600 dark:text-amber-400" aria-hidden />;
  }
  if (conclusion === "neutral" || conclusion === "skipped" || conclusion === "cancelled") {
    return <CircleDashed className="size-4 text-muted-foreground" aria-hidden />;
  }
  return <HelpCircle className="size-4 text-muted-foreground" aria-hidden />;
}

function ReviewBadge({ state }: { state: string }) {
  const { t } = useT("ship");
  const upper = state.toUpperCase();
  if (upper === "APPROVED") {
    return (
      <Badge variant="outline" className="border-emerald-500/40 text-emerald-700 dark:text-emerald-400">
        {t(($) => $.pr_detail.review_state_approved)}
      </Badge>
    );
  }
  if (upper === "CHANGES_REQUESTED") {
    return (
      <Badge variant="outline" className="border-orange-500/40 text-orange-700 dark:text-orange-400">
        {t(($) => $.pr_detail.review_state_changes_requested)}
      </Badge>
    );
  }
  if (upper === "DISMISSED") {
    return <Badge variant="outline">{t(($) => $.pr_detail.review_state_dismissed)}</Badge>;
  }
  if (upper === "PENDING") {
    return <Badge variant="outline">{t(($) => $.pr_detail.review_state_pending)}</Badge>;
  }
  // COMMENTED + every unknown enum lands on the same neutral chip.
  return <Badge variant="outline">{t(($) => $.pr_detail.review_state_commented)}</Badge>;
}

/** A short header above each section. Keeps the visual rhythm
 *  consistent across the drawer's content stack. */
function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="mt-4 mb-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
      {children}
    </div>
  );
}

interface PrRefRowProps {
  pr: DrawerPullRequestRef;
  slug: string;
}

/** Renders a single neighbour-PR (parent or child) row. Uses a button
 *  shape so a future "open this PR's drawer" affordance can land
 *  here without restructuring. v1: external GitHub link only. */
function PrRefRow({ pr, slug: _slug }: PrRefRowProps) {
  return (
    <a
      href={pr.html_url}
      target="_blank"
      rel="noopener noreferrer"
      className="group flex items-center gap-2 rounded border bg-muted/30 px-2 py-1.5 text-xs hover:bg-accent"
    >
      <GitPullRequest className="size-3.5 text-muted-foreground" aria-hidden />
      <span className="tabular-nums text-muted-foreground">#{pr.number}</span>
      <span className="truncate text-foreground">{pr.title}</span>
      <ExternalLink
        className="ml-auto size-3 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100"
        aria-hidden
      />
    </a>
  );
}

/** Maps an action enum string to a friendly label. Falls back to the
 *  raw string for unknown values per CLAUDE.md "Enum drift downgrades,
 *  not crashes." */
function actionLabel(action: string): string {
  switch (action) {
    case "merge":
      return "Merged";
    case "rebase_on_main":
      return "Rebased on main";
    case "comment":
      return "Comment posted";
    case "dismiss_review":
      return "Review dismissed";
    case "diagnose_ci_failure":
      return "Diagnosed CI failure";
    case "summarize_review_feedback":
      return "Summarized reviews";
    case "nudge_author":
      return "Nudged author";
    case "run_smoke_tests":
      return "Smoke tests started";
    case "close_as_stale":
      return "Closed as stale";
    case "submit_review":
      return "Review submitted";
    default:
      return action;
  }
}

interface DrawerHeaderProps {
  pr: PullRequest;
  locale: string;
}

function DrawerHeader({ pr, locale }: DrawerHeaderProps) {
  const { t } = useT("ship");
  // Prefer pr_merged_at for description when present — same logic as
  // the card. Closed (not merged) renders the close timestamp; open
  // renders the create date.
  let descKey: "drawer_description_merged" | "drawer_description_closed" | "drawer_description_open" =
    "drawer_description_open";
  let when = pr.pr_created_at;
  if (pr.state === "merged" && pr.pr_merged_at) {
    descKey = "drawer_description_merged";
    when = pr.pr_merged_at;
  } else if (pr.state === "closed" && pr.pr_closed_at) {
    descKey = "drawer_description_closed";
    when = pr.pr_closed_at;
  }
  // Mirror the Kanban card's selection contract so a user can multi-
  // select PRs from inside the drawer too. Without this, opening the
  // drawer to confirm a PR's details forced the user to close it,
  // hover the card, find the checkbox, then re-open the drawer for
  // the next candidate. Now the same checkbox is available alongside
  // the close button — same store, same behavior.
  const isSelected = useShipSelection((s) => s.selected.has(pr.id));
  const toggleSelection = useShipSelection((s) => s.toggle);
  return (
    <SheetHeader className="border-b px-4 py-3">
      <div className="flex items-start gap-3">
        {pr.author_avatar_url ? (
          <img
            src={pr.author_avatar_url}
            alt=""
            aria-hidden
            className="mt-0.5 size-6 shrink-0 rounded-full"
          />
        ) : (
          <GitPullRequest className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
        )}
        <div className="min-w-0 flex-1">
          <SheetTitle className="line-clamp-2 break-words pr-8 text-base">
            {pr.title}
          </SheetTitle>
          <SheetDescription>
            <span className="tabular-nums">#{pr.number}</span> ·{" "}
            {t(($) => $.pr_detail[descKey], {
              author: pr.author_login,
              when: formatRelativeTime(when, locale),
            })}
          </SheetDescription>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <label
              className={cn(
                "inline-flex cursor-pointer items-center gap-1.5 rounded border px-2 py-1 text-[11px]",
                isSelected
                  ? "border-primary/50 bg-primary/10 text-primary"
                  : "hover:bg-accent",
              )}
              data-testid="ship-pr-detail-select-for-release"
            >
              <Checkbox
                checked={isSelected}
                onCheckedChange={() => toggleSelection(pr.id)}
                aria-label={t(($) => $.pr_detail.select_for_release_aria, {
                  number: pr.number,
                })}
                className="size-3.5"
              />
              {t(($) =>
                isSelected
                  ? $.pr_detail.deselect_for_release
                  : $.pr_detail.select_for_release,
              )}
            </label>
            <a
              href={pr.html_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 rounded border px-2 py-1 text-[11px] hover:bg-accent"
              data-testid="ship-pr-detail-view-on-github"
            >
              <ExternalLink className="size-3" aria-hidden />
              {t(($) => $.pr_detail.view_on_github)}
            </a>
            <a
              href={`${pr.html_url}/files`}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 rounded border px-2 py-1 text-[11px] hover:bg-accent"
            >
              <ExternalLink className="size-3" aria-hidden />
              {t(($) => $.pr_detail.view_diff)}
            </a>
          </div>
        </div>
      </div>
    </SheetHeader>
  );
}

function ChecksList({ checks, locale }: { checks: DrawerCheck[]; locale: string }) {
  const { t } = useT("ship");
  if (checks.length === 0) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="drawer-no-checks">
        {t(($) => $.pr_detail.no_checks)}
      </p>
    );
  }
  return (
    <ul className="flex flex-col gap-1.5">
      {checks.map((check) => (
        <li key={check.id} className="flex items-center gap-2 text-xs">
          <CheckIcon check={check} />
          <span className="truncate text-foreground">{check.name}</span>
          <span className="ml-auto text-[11px] text-muted-foreground">
            {check.completed_at
              ? t(($) => $.pr_detail.completed_relative, {
                  when: formatRelativeTime(check.completed_at, locale),
                })
              : check.started_at
              ? t(($) => $.pr_detail.started_relative, {
                  when: formatRelativeTime(check.started_at, locale),
                })
              : ""}
          </span>
          {check.details_url && (
            <a
              href={check.details_url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-muted-foreground hover:text-foreground"
              aria-label={`View run for ${check.name}`}
            >
              <ExternalLink className="size-3" aria-hidden />
            </a>
          )}
        </li>
      ))}
    </ul>
  );
}

function ReviewsList({
  reviews,
  locale,
}: {
  reviews: DrawerReview[];
  locale: string;
}) {
  const { t } = useT("ship");
  if (reviews.length === 0) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="drawer-no-reviews">
        {t(($) => $.pr_detail.no_reviews)}
      </p>
    );
  }
  return (
    <ul className="flex flex-col gap-2">
      {reviews.map((rv) => (
        <li
          key={rv.id}
          className="rounded border bg-muted/30 p-2 text-xs"
        >
          <div className="flex items-center gap-2">
            {rv.reviewer_avatar_url ? (
              <img
                src={rv.reviewer_avatar_url}
                alt=""
                aria-hidden
                className="size-4 rounded-full"
              />
            ) : null}
            <span className="font-medium text-foreground">{rv.reviewer_login}</span>
            <ReviewBadge state={rv.state} />
            <span className="ml-auto text-[11px] text-muted-foreground">
              {rv.submitted_at
                ? t(($) => $.pr_detail.submitted_relative, {
                    when: formatRelativeTime(rv.submitted_at, locale),
                  })
                : ""}
            </span>
          </div>
          {rv.body && rv.body.trim().length > 0 && (
            <p className="mt-1 line-clamp-3 whitespace-pre-wrap break-words text-muted-foreground">
              {rv.body}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}

function ActionsList({
  actions,
  locale,
}: {
  actions: DrawerCardAction[];
  locale: string;
}) {
  const { t } = useT("ship");
  if (actions.length === 0) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="drawer-no-recent-actions">
        {t(($) => $.pr_detail.no_recent_actions)}
      </p>
    );
  }
  return (
    <ul className="flex flex-col gap-1.5">
      {actions.map((action) => (
        <li
          key={action.id}
          className={cn(
            "flex items-center gap-2 text-xs",
            action.result_status === "failed" && "text-destructive",
          )}
        >
          <span className="truncate text-foreground">{actionLabel(action.action)}</span>
          <Badge
            variant="outline"
            className={cn(
              "text-[10px]",
              action.result_status === "succeeded" &&
                "border-emerald-500/40 text-emerald-700 dark:text-emerald-400",
              action.result_status === "failed" &&
                "border-destructive/40 text-destructive",
            )}
          >
            {action.result_status}
          </Badge>
          <span className="ml-auto text-[11px] text-muted-foreground">
            {formatRelativeTime(action.completed_at ?? action.created_at, locale)}
          </span>
        </li>
      ))}
    </ul>
  );
}

interface DrawerBodyProps {
  prId: string;
}

/** DrawerBody — fetches /details for the open PR and renders sections.
 *  Split out from the wrapper so the Sheet's open/close transitions
 *  don't trigger a refetch every cycle. */
function DrawerBody({ prId }: DrawerBodyProps) {
  const { t, i18n } = useT("ship");
  const workspace = useCurrentWorkspace();
  const slug = workspace?.slug ?? "";
  const { data, isLoading, isError } = usePullRequestDetails(prId);

  if (isLoading || !data) {
    return (
      <div className="flex h-32 items-center justify-center gap-2 text-sm text-muted-foreground" data-testid="drawer-loading">
        <Spinner className="size-4" aria-hidden />
        {t(($) => $.pr_detail.loading)}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="px-4 py-6 text-sm text-destructive" data-testid="drawer-error">
        {t(($) => $.pr_detail.error_loading)}
      </div>
    );
  }

  const pr = data.pull_request;
  const locale = i18n.language;
  return (
    <>
      <DrawerHeader pr={pr} locale={locale} />
      <DrawerSections data={data} pr={pr} slug={slug} locale={locale} />
    </>
  );
}

interface DrawerSectionsProps {
  data: NonNullable<ReturnType<typeof usePullRequestDetails>["data"]>;
  pr: PullRequest;
  slug: string;
  locale: string;
}

function DrawerSections({ data, pr, slug, locale }: DrawerSectionsProps) {
  const { t } = useT("ship");

  // Has-something predicates so each section can hide cleanly. We keep
  // these inline (not memoised) — the array reads are cheap and React
  // re-renders are cheap; the readability win matters more.
  const hasLinkedSection =
    !!data.linked_issue ||
    !!data.originating_agent_task ||
    !!data.conversation_channel ||
    !!data.active_release;
  const hasStackSection = !!data.stack_parent || data.stack_children.length > 0;

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="space-y-1 px-4 py-3">
        {/* Source — repo, branches, head SHA. Always rendered because
            every PR has a repo and head sha. */}
        <SectionHeader>{t(($) => $.pr_detail.section_source)}</SectionHeader>
        <div className="flex flex-col gap-1 text-xs">
          <a
            href={pr.repo_url}
            target="_blank"
            rel="noopener noreferrer"
            className="truncate text-foreground hover:underline"
          >
            {pr.repo_url}
          </a>
          <div className="text-muted-foreground">
            {t(($) => $.pr_detail.head_to_base, {
              base: pr.base_ref,
              head: pr.head_ref,
            })}
          </div>
          <div className="flex items-center gap-2 text-muted-foreground">
            <span>{t(($) => $.pr_detail.head_sha_label)}:</span>
            <a
              href={`${pr.repo_url}/commit/${pr.head_sha}`}
              target="_blank"
              rel="noopener noreferrer"
              className="font-mono text-[11px] hover:underline"
            >
              {pr.head_sha.slice(0, 7)}
            </a>
            {(pr.additions > 0 || pr.deletions > 0 || pr.changed_files > 0) && (
              <span className="ml-auto tabular-nums">
                {t(($) => $.card.additions_deletions, {
                  additions: pr.additions,
                  deletions: pr.deletions,
                })}
                {" · "}
                {t(($) => $.card.files_count, { count: pr.changed_files })}
              </span>
            )}
          </div>
        </div>

        {/* Linked Multica entities. Renders only when at least one
            field exists — keeps the drawer clean for raw GitHub PRs. */}
        {hasLinkedSection && (
          <>
            <SectionHeader>{t(($) => $.pr_detail.section_linked)}</SectionHeader>
            <div className="flex flex-wrap gap-1.5">
              {data.linked_issue && slug && (
                <AppLink
                  href={`/${slug}/issues/${data.linked_issue.identifier}`}
                  className="inline-flex items-center gap-1 rounded bg-blue-500/10 px-1.5 py-0.5 text-[11px] font-medium text-blue-700 hover:bg-blue-500/20 dark:text-blue-400"
                  data-testid="drawer-linked-issue"
                >
                  <Hash className="size-3" aria-hidden />
                  {t(($) => $.pr_detail.linked_issue_chip, {
                    identifier: data.linked_issue.identifier,
                  })}
                </AppLink>
              )}
              {data.originating_agent_task && (
                <span
                  className="inline-flex items-center gap-1 rounded bg-purple-500/10 px-1.5 py-0.5 text-[11px] font-medium text-purple-700 dark:text-purple-400"
                  data-testid="drawer-linked-agent"
                >
                  <Bot className="size-3" aria-hidden />
                  {t(($) => $.pr_detail.linked_agent_chip, {
                    name: data.originating_agent_task.agent_name || "agent",
                  })}
                </span>
              )}
              {data.conversation_channel && slug && (
                <AppLink
                  href={`/${slug}/channels/${data.conversation_channel.name}`}
                  className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent"
                  data-testid="drawer-linked-channel"
                >
                  <MessagesSquare className="size-3" aria-hidden />
                  {t(($) => $.pr_detail.linked_channel_chip, {
                    name: data.conversation_channel.name,
                  })}
                </AppLink>
              )}
              {data.active_release && slug && (
                <AppLink
                  href={`/${slug}/ship/release/${data.active_release.id}`}
                  className="inline-flex items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-[11px] font-medium text-primary hover:bg-primary/20"
                  data-testid="drawer-linked-release"
                >
                  <Train className="size-3" aria-hidden />
                  {t(($) => $.pr_detail.linked_release_chip, {
                    title: data.active_release.title,
                  })}
                </AppLink>
              )}
            </div>
          </>
        )}

        {/* Description. Rendered as preformatted text rather than
            markdown — pulling in a markdown library for one surface
            isn't worth the bundle cost; readers who want the rich
            view click "View on GitHub". */}
        <SectionHeader>{t(($) => $.pr_detail.section_description)}</SectionHeader>
        {pr.body && pr.body.trim().length > 0 ? (
          <pre
            data-testid="drawer-description"
            className="whitespace-pre-wrap break-words rounded border bg-muted/20 p-2 font-sans text-xs leading-relaxed text-foreground"
          >
            {pr.body}
          </pre>
        ) : (
          <p className="text-xs italic text-muted-foreground">
            {t(($) => $.pr_detail.no_description)}
          </p>
        )}

        {/* Stack neighbours. Renders only when at least one of
            parent/children is populated. */}
        {hasStackSection && (
          <>
            <SectionHeader>{t(($) => $.pr_detail.section_stack)}</SectionHeader>
            {data.stack_parent && (
              <div className="mb-2">
                <div className="mb-1 text-[11px] text-muted-foreground">
                  {t(($) => $.pr_detail.stack_parent_label)}
                </div>
                <PrRefRow pr={data.stack_parent} slug={slug} />
              </div>
            )}
            {data.stack_children.length > 0 && (
              <div>
                <div className="mb-1 text-[11px] text-muted-foreground">
                  {t(($) => $.pr_detail.stack_children_label)}
                </div>
                <div className="flex flex-col gap-1.5">
                  {data.stack_children.map((child) => (
                    <PrRefRow key={child.id} pr={child} slug={slug} />
                  ))}
                </div>
              </div>
            )}
          </>
        )}

        {/* CI checks. */}
        <SectionHeader>{t(($) => $.pr_detail.section_checks)}</SectionHeader>
        <ChecksList checks={data.checks} locale={locale} />

        {/* Reviews. */}
        <SectionHeader>{t(($) => $.pr_detail.section_reviews)}</SectionHeader>
        <ReviewsList reviews={data.reviews} locale={locale} />

        {/* Recent ship_card_action audit tail. Truncated at 10 by the
            backend; the release page's audit view holds the full
            trail. */}
        <SectionHeader>{t(($) => $.pr_detail.section_recent_actions)}</SectionHeader>
        <ActionsList actions={data.recent_actions} locale={locale} />
      </div>
    </ScrollArea>
  );
}

/** ShipPrDetailDrawer — the wrapper that subscribes to the open-PR
 *  store and lazy-mounts the body. Mount once per Ship Hub surface
 *  (the page wraps the Kanban + release listing); the store decides
 *  whether the Sheet is visible.
 *
 *  Sheet's open/close transition animates regardless of whether the
 *  body is mounted, so we keep the body conditionally mounted: when
 *  closed, no fetch; when open, the bundled query fires and the
 *  drawer renders within ~300ms of click. */
export function ShipPrDetailDrawer() {
  const openPrId = useShipPrDetailOpenId();
  const close = useShipPrDetailStore((s) => s.close);
  const open = openPrId !== null;

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) close();
      }}
    >
      <SheetContent
        side="right"
        // `overflow-hidden` clips at the Sheet level and `min-h-0` on
        // the inner ScrollArea below lets the flex-1 child actually
        // constrain — without these, a long PR description (or large
        // checks/reviews list) made the drawer body grow past the
        // viewport and the user couldn't reach lower sections. With
        // them, the inner ScrollArea handles overflow with a styled
        // scrollbar.
        className="flex h-full w-full flex-col gap-0 overflow-hidden sm:max-w-xl"
        data-testid="ship-pr-detail-drawer"
      >
        {openPrId ? <DrawerBody prId={openPrId} /> : null}
      </SheetContent>
    </Sheet>
  );
}
