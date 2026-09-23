"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, ChevronRight } from "lucide-react";
import type { Issue, IssueDuplicateOf } from "@multica/core/types";
import { issueStatusCategory } from "@multica/core/issues";
import { issueDuplicatesOptions } from "@multica/core/issues/queries";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, resolveClickIntent, rowLinkInteractiveProps, useIntentNavigate } from "../../navigation";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";

// Duplicate marks (MUL-7349). A duplicate is a cancelled issue that remembers
// its original; the server clears the mark whenever the status leaves
// cancelled, so every surface below also keys off the status.

/** True while the mark counts: the issue is cancelled and the server resolved its original. */
export function isDuplicateIssue(issue: Pick<Issue, "status" | "duplicate_of">): boolean {
  return issue.status === "cancelled" && !!issue.duplicate_of;
}

/**
 * Banner above a duplicate's title. Its one job is to send the reader on:
 * the whole strip links to the original and shows the original's own status,
 * so "is that one done yet?" is answered before the click. Removing the mark
 * is the rare correction, so it stays a quiet secondary action; the write is a
 * plain status change to todo, done through the caller so it shares the
 * detail page's status path.
 */
export function IssueDuplicateBanner({
  issue,
  onUnmark,
}: {
  issue: Issue;
  onUnmark: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { colorOf, iconOf } = useIssueStatuses(wsId);
  const { data } = useQuery(issueDuplicatesOptions(wsId, issue.id));
  const original = data?.duplicate_of;
  if (issue.status !== "cancelled" || !original) return null;

  return (
    <div className="mb-4 flex items-center gap-1 rounded-md border bg-muted/40 py-1 pl-3 pr-1 text-body">
      <AppLink
        href={paths.issueDetail(original.id)}
        newTabTitle={original.identifier}
        className="group flex min-w-0 flex-1 items-center gap-2 py-1"
      >
        <span className="shrink-0 text-muted-foreground">
          {t(($) => $.duplicates.banner_prefix)}
        </span>{" "}
        <StatusIcon
          status={original.status}
          color={colorOf(original.status)}
          icon={iconOf(original.status)}
          category={issueStatusCategory(original) ?? undefined}
          className="h-3.5 w-3.5 shrink-0"
        />
        <span className="shrink-0 text-muted-foreground">{original.identifier}</span>{" "}
        <span className="truncate font-medium underline-offset-4 group-hover:underline">
          {original.title}
        </span>
        <ArrowRight className="ml-auto h-4 w-4 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground" />
      </AppLink>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="shrink-0 text-muted-foreground"
        onClick={onUnmark}
      >
        {t(($) => $.duplicates.unmark)}
      </Button>
    </div>
  );
}

/**
 * Sidebar list of the issues marked as duplicates of this one. Every entry is
 * cancelled, so a status icon would say the same thing on each row; the
 * reporter is the signal — several faces means several people hit this.
 */
export function IssueDuplicatesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [open, setOpen] = useState(true);
  const { data } = useQuery(issueDuplicatesOptions(wsId, issueId));
  const duplicates = data?.duplicates ?? [];
  if (duplicates.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.duplicates.section_title)}{" "}
        <span className="rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground">
          {duplicates.length}
        </span>
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2">
          {duplicates.map((duplicate) => (
            <AppLink
              key={duplicate.id}
              href={paths.issueDetail(duplicate.id)}
              className="group flex items-center gap-1.5 rounded-md px-2 -mx-2 py-1.5 text-caption hover:bg-accent/50 transition-colors"
            >
              <ActorAvatar
                actorType={duplicate.creator_type}
                actorId={duplicate.creator_id}
                size="xs"
                profileLink={false}
              />
              <span className="text-muted-foreground shrink-0">{duplicate.identifier}</span>
              <span className="truncate group-hover:text-foreground">{duplicate.title}</span>
            </AppLink>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * "→ MUL-123" beside a duplicate in list rows, board cards and table cells,
 * so the original is one click away instead of two. The server resolves the
 * original into `issue.duplicate_of`, so this needs no request of its own,
 * and rows that are not duplicates render nothing.
 *
 * It is a real link wherever the row is not one itself (the table's rows are
 * plain elements). List rows and board cards are anchors, and an anchor may
 * not contain another, so there it is a keyboard-operable control that
 * navigates like a link and stops the row from also opening the duplicate.
 */
export function IssueDuplicateOfMarker({
  issue,
  className,
  insideLink = false,
}: {
  issue: Issue;
  className?: string;
  /** The row containing this marker is itself an anchor. */
  insideLink?: boolean;
}) {
  if (!isDuplicateIssue(issue)) return null;
  const original = issue.duplicate_of!;
  return insideLink ? (
    <DuplicateOfControl original={original} className={className} />
  ) : (
    <DuplicateOfAnchor original={original} className={className} />
  );
}

const MARKER_CLASS =
  "inline-flex shrink-0 cursor-pointer items-center gap-0.5 text-caption text-muted-foreground underline-offset-4 hover:text-foreground hover:underline";

function useMarkerTooltip(original: IssueDuplicateOf) {
  const { t } = useT("issues");
  return `${t(($) => $.duplicates.banner_prefix)} ${original.identifier} ${original.title}`;
}

const stopPress = (e: React.SyntheticEvent) => e.stopPropagation();

function DuplicateOfAnchor({
  original,
  className,
}: {
  original: IssueDuplicateOf;
  className?: string;
}) {
  const paths = useWorkspacePaths();
  const tooltip = useMarkerTooltip(original);
  return (
    <AppLink
      href={paths.issueDetail(original.id)}
      newTabTitle={original.identifier}
      title={tooltip}
      {...rowLinkInteractiveProps}
      onMouseDown={stopPress}
      onPointerDown={stopPress}
      className={`${MARKER_CLASS} ${className ?? ""}`}
    >
      <ArrowRight className="size-3" />
      {original.identifier}
    </AppLink>
  );
}

function DuplicateOfControl({
  original,
  className,
}: {
  original: IssueDuplicateOf;
  className?: string;
}) {
  const paths = useWorkspacePaths();
  const navigate = useIntentNavigate();
  const tooltip = useMarkerTooltip(original);
  const href = paths.issueDetail(original.id);
  const stop = (e: React.SyntheticEvent) => {
    e.preventDefault();
    e.stopPropagation();
  };
  return (
    <span
      role="link"
      tabIndex={0}
      title={tooltip}
      onMouseDown={stopPress}
      onPointerDown={stopPress}
      onClick={(e) => {
        stop(e);
        navigate(href, resolveClickIntent(e), original.identifier);
      }}
      onAuxClick={(e) => {
        if (e.button !== 1) return;
        stop(e);
        navigate(href, "background-tab", original.identifier);
      }}
      onKeyDown={(e) => {
        if (e.key !== "Enter") return;
        stop(e);
        navigate(href, "push", original.identifier);
      }}
      className={`${MARKER_CLASS} ${className ?? ""}`}
    >
      <ArrowRight className="size-3" />
      {original.identifier}
    </span>
  );
}
