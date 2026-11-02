"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  issueTipsOptions,
  useYouShouldKnowEnabled,
  type IssueTip,
} from "@multica/core/you-should-know";
import { Input } from "@multica/ui/components/ui/input";
import { useLocale, useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { AskAboutTipButton } from "../side-chat/ask-about-tip-button";

type TipFilter = "valid" | "invalid";

// The feature's visual mark (matches the bubble card). Not copy — never
// localized; held in a constant so the i18next jsx-text guard doesn't read it
// as untranslated copy.
const TIP_MARK = "✦";

interface TipGroup {
  /** `anchor_comment_id`, or "" for the issue-description group. */
  key: string;
  commentId: string | null;
  /** Earliest tip creation time in the group (ISO); the label's fallback. */
  time: string;
  /**
   * Time the group sorts and labels by (ISO): the anchor comment's own
   * creation time when `commentTime` resolves it, else `time`. Computed once
   * here so the order can never disagree with the rendered label (OL-387).
   */
  labelTime: string;
  tips: IssueTip[];
}

/**
 * Group tips by their anchor comment. The issue-description group is keyed by
 * "" and keeps `commentId: null`. Tips inside a group stay in server order
 * (newest first); groups themselves sort newest-first by their label time —
 * the anchor comment's creation time when known, else the group's earliest
 * tip (OL-387, superseding OL-195's earliest-tip order).
 */
export function groupTips(
  tips: readonly IssueTip[],
  commentTime?: (commentId: string) => string | undefined,
): TipGroup[] {
  const groups = new Map<string, TipGroup>();
  for (const tip of tips) {
    const key = tip.anchor_comment_id ?? "";
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        commentId: tip.anchor_comment_id,
        time: tip.created_at,
        // Placeholder to satisfy the type; recomputed for every group below
        // once `time` is final.
        labelTime: tip.created_at,
        tips: [],
      };
      groups.set(key, group);
    }
    group.tips.push(tip);
    // RFC3339Nano strings don't order lexicographically (trailing fractional
    // zeros are trimmed), so compare as instants via Date.parse.
    const tipMs = Date.parse(tip.created_at);
    const groupMs = Date.parse(group.time);
    if (!Number.isNaN(tipMs) && (Number.isNaN(groupMs) || tipMs < groupMs)) {
      group.time = tip.created_at;
    }
  }
  for (const group of groups.values()) {
    group.labelTime = labelTimeOf(group, commentTime);
  }
  return [...groups.values()].sort(
    (a, b) => Date.parse(b.labelTime) - Date.parse(a.labelTime),
  );
}

/**
 * The group's label time: the anchor comment's own creation time when the
 * lookup resolves to a parseable instant, else the group's earliest tip.
 */
function labelTimeOf(
  group: TipGroup,
  commentTime?: (commentId: string) => string | undefined,
): string {
  const commentAt = group.commentId ? commentTime?.(group.commentId) : undefined;
  return commentAt && !Number.isNaN(Date.parse(commentAt)) ? commentAt : group.time;
}

function tipMatchesFilter(tip: IssueTip, filter: TipFilter): boolean {
  return filter === "valid" ? tip.status === "valid" : tip.status !== "valid";
}

/** Case-insensitive match over the tip's line, title and explanation. */
function tipMatchesQuery(tip: IssueTip, needle: string): boolean {
  if (!needle) return true;
  return [tip.line, tip.title ?? "", tip.explanation ?? ""].some((field) =>
    field.toLocaleLowerCase().includes(needle),
  );
}

/**
 * The issue sidebar's You should know archive (OL-187 §7): every tip agents
 * left on this issue, grouped by the comment they are anchored to, with a
 * valid/no-longer-valid filter and search within the issue. Renders nothing
 * while either switch is off or the issue has no tips at all.
 */
export function YouShouldKnowArchiveSection({
  issueId,
  identifier,
  onJumpToComment,
  commentTime,
}: {
  issueId: string;
  /** Used to build comment deep links; links are skipped without it. */
  identifier?: string;
  /**
   * Jump to a comment inside this issue page. The mount point passes the
   * timeline's jump helper, which handles virtualized, collapsed and
   * resolved threads — a plain URL hash change scrolls nothing there.
   * Falls back to a URL push when absent.
   */
  onJumpToComment?: (commentId: string) => void;
  /**
   * Comment creation time (ISO) by comment id, from the issue's loaded
   * timeline. Comment groups sort and label by it so two same-day comment
   * groups stay distinguishable and the order can never disagree with the
   * labels; ids outside the map fall back to the group's earliest tip time.
   */
  commentTime?: (commentId: string) => string | undefined;
}) {
  const { t } = useT("you-should-know");
  const wsId = useWorkspaceId();
  const { enabled } = useYouShouldKnowEnabled();
  const { data } = useQuery({ ...issueTipsOptions(wsId, issueId, "all"), enabled });
  const [open, setOpen] = useState(true);
  const [filter, setFilter] = useState<TipFilter>("valid");
  const [query, setQuery] = useState("");

  const tips = useMemo(() => data?.tips ?? [], [data]);
  const groups = useMemo(() => groupTips(tips, commentTime), [tips, commentTime]);

  if (!enabled || tips.length === 0) return null;

  const needle = query.trim().toLocaleLowerCase();
  const visible = groups
    .map((group) => ({
      ...group,
      tips: group.tips.filter(
        (tip) => tipMatchesFilter(tip, filter) && tipMatchesQuery(tip, needle),
      ),
    }))
    .filter((group) => group.tips.length > 0);

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
        onClick={() => setOpen(!open)}
      >
        <span className="truncate">{t(($) => $.archive.title)}</span>
        <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground">
          {tips.length}
        </span>
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="space-y-2 pl-2">
          <div className="flex items-center gap-1">
            {(["valid", "invalid"] as const).map((value) => (
              <button
                key={value}
                type="button"
                aria-pressed={filter === value}
                className={`rounded-md px-1.5 py-0.5 text-caption font-medium transition-colors ${
                  filter === value
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
                onClick={() => setFilter(value)}
              >
                {t(($) =>
                  value === "valid"
                    ? $.archive.filter_valid
                    : $.archive.filter_invalid,
                )}
              </button>
            ))}
            <Input
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.archive.search_placeholder)}
              aria-label={t(($) => $.archive.search_placeholder)}
              className="h-6 flex-1 px-2 text-caption"
            />
          </div>
          {visible.length === 0 ? (
            <p className="px-1 text-caption text-muted-foreground">
              {t(($) => $.archive.no_results)}
            </p>
          ) : (
            visible.map((group) => (
              <ArchiveGroup
                key={group.key || "description"}
                group={group}
                identifier={identifier}
                onJumpToComment={onJumpToComment}
              />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ArchiveGroup({
  group,
  identifier,
  onJumpToComment,
}: {
  group: TipGroup;
  identifier?: string;
  onJumpToComment?: (commentId: string) => void;
}) {
  const { t } = useT("you-should-know");
  const locale = useLocale();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  // The group header jumps to the comment its tips are anchored to; the
  // issue-description group has no comment to jump to and renders as text.
  const href =
    group.commentId !== null && identifier
      ? `${paths.issueDetail(identifier)}#comment-${group.commentId}`
      : null;
  // The label renders the group's labelTime — the anchor comment's own
  // creation time down to the minute when known, else the group's earliest
  // tip time — which is also the sort key (OL-346, OL-387). Like the comment
  // card, the instant renders in the viewer's timezone; empty or unparseable
  // values render no date rather than "Invalid Date".
  const timeMs = group.labelTime ? Date.parse(group.labelTime) : NaN;
  const when = Number.isNaN(timeMs)
    ? ""
    : new Date(timeMs).toLocaleString(locale, {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      });
  const label =
    group.commentId === null
      ? t(($) => $.archive.group_description)
      : when
        ? `${t(($) => $.archive.group_comment)} · ${when}`
        : t(($) => $.archive.group_comment);

  const jump = () => {
    if (!group.commentId) return;
    if (onJumpToComment) onJumpToComment(group.commentId);
    else if (href) navigation.push(href);
  };

  return (
    <div>
      {href ? (
        <button
          type="button"
          className="flex w-full items-center rounded-xs px-1 py-0.5 text-left text-micro font-medium text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
          onClick={jump}
        >
          <span className="truncate">{label}</span>
        </button>
      ) : (
        <p className="px-1 py-0.5 text-micro font-medium text-muted-foreground">
          {label}
        </p>
      )}
      <div className="space-y-0.5">
        {group.tips.map((tip) => (
          <ArchiveTip
            key={tip.id}
            tip={tip}
            identifier={identifier}
            onJumpToComment={onJumpToComment}
          />
        ))}
      </div>
    </div>
  );
}

function ArchiveTip({
  tip,
  identifier,
  onJumpToComment,
}: {
  tip: IssueTip;
  identifier?: string;
  onJumpToComment?: (commentId: string) => void;
}) {
  const { t } = useT("you-should-know");
  const [expanded, setExpanded] = useState(false);
  const invalid = tip.status !== "valid";
  const tagLabel = t(($) =>
    tip.tag === "heads_up" ? $.tag.heads_up : $.tag.you_should_know,
  );

  return (
    <>
      <div data-invalid={invalid || undefined} className={invalid ? "opacity-60" : undefined}>
        {/* Collapsed: "✦ tag · one-liner". Clicking reveals title+explanation. */}
        <button
          type="button"
          aria-expanded={expanded}
          className="flex w-full items-start gap-1 rounded-xs px-1 py-1 text-left text-caption transition-colors hover:bg-accent/50"
          onClick={() => setExpanded(!expanded)}
        >
          <span aria-hidden className="shrink-0">{TIP_MARK}</span>
          <span className="shrink-0 font-medium">{tagLabel}</span>
          <span aria-hidden className="shrink-0 text-faint-foreground">·</span>
          <span className="min-w-0 flex-1">{tip.line}</span>
        </button>
        {expanded && (
          <div className="pb-1 pl-6 pr-1 text-caption text-muted-foreground">
            {tip.title ? (
              <p className="font-medium text-foreground">{tip.title}</p>
            ) : null}
            {tip.explanation ? <p>{tip.explanation}</p> : null}
          </div>
        )}
        {invalid && (
          <InvalidNote
            tip={tip}
            identifier={identifier}
            onJumpToComment={onJumpToComment}
          />
        )}
      </div>
      {/* Every tip can be asked about, the no-longer-valid ones too (OL-288
          §2) — outside the greyed block, which would read as disabled. */}
      {expanded && (
        <div className="pb-1 pl-4">
          <AskAboutTipButton tip={tip} />
        </div>
      )}
    </>
  );
}

/** Why an overturned / source-changed tip stopped being valid. */
function InvalidNote({
  tip,
  identifier,
  onJumpToComment,
}: {
  tip: IssueTip;
  identifier?: string;
  onJumpToComment?: (commentId: string) => void;
}) {
  const { t } = useT("you-should-know");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  if (tip.status === "source_changed") {
    return <Note>{t(($) => $.archive.source_changed)}</Note>;
  }
  const commentId = tip.overturned_by_comment_id;
  const text = t(($) =>
    commentId ? $.archive.overturned_by_comment : $.archive.overturned,
  );
  const jump = () => {
    if (!commentId) return;
    if (onJumpToComment) onJumpToComment(commentId);
    else if (identifier)
      navigation.push(`${paths.issueDetail(identifier)}#comment-${commentId}`);
  };
  return (
    <Note>
      {commentId && identifier ? (
        <button
          type="button"
          className="underline underline-offset-2 hover:text-foreground"
          onClick={jump}
        >
          {text}
        </button>
      ) : (
        text
      )}
      {tip.overturned_reason ? <span> · {tip.overturned_reason}</span> : null}
    </Note>
  );
}

function Note({ children }: { children: ReactNode }) {
  return <p className="pb-1 pl-6 pr-1 text-micro text-muted-foreground">{children}</p>;
}
