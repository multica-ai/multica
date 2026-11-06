"use client";

import { useMemo } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { issueStatusListOptions, buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import {
  parseTabSubject,
  resolveTabPresentation,
  useCurrentWorkspace,
  type TabSubject,
  type TabVisual,
  type TabTitleSpec,
  type TabEntityData,
  type TabLabelKey,
} from "@multica/core/paths";
import { useCanonicalIssue } from "@multica/core/issues/canonical-id";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { autopilotDetailOptions } from "@multica/core/autopilots/queries";
import {
  skillDetailOptions,
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { runtimeDisplayName } from "@multica/core/runtimes";
import { chatSessionOptions } from "@multica/core/chat/queries";
import {
  inboxListOptions,
  archivedInboxPagesOptions,
  archivedInboxLookupOptions,
} from "@multica/core/inbox/queries";
import { useInboxFilters } from "@multica/core/inbox/filter-store";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { StatusIcon } from "../issues/components";
import { ProjectIcon } from "../projects/components/project-icon";
import { ActorAvatar } from "../common/actor-avatar";
import { getInboxDisplayTitle } from "../inbox/components/inbox-display";
import { useT } from "../i18n";
import { ROUTE_ICON_COMPONENTS } from "./route-icon-components";

/**
 * Desktop tab presentation: turn a tab URL into a leading visual and a title,
 * live from the query cache. This is the view half of the contract whose pure
 * core is `@multica/core/paths` (`parseTabSubject` + `resolveTabPresentation`).
 *
 * Reads are cache-only by default. The desktop tab strip opts into fetching
 * issue details and the status catalog so restored background tabs resolve
 * without mounting their pages. Other resources observe the pages' caches and
 * keep their type fallback until those pages load.
 *
 * The one exception is an actor tab's avatar: `ResourceLeadingVisual` renders
 * `ActorAvatar`, which loads the (workspace-global, sidebar-warmed) member /
 * agent / squad directories itself. That is intentional — it resolves the
 * avatar and, in turn, the name this hook reads from the same lists.
 */

// Placeholder id for a detail query that this tab doesn't need — its key is
// never populated, so the read returns undefined without any fetch.
const NONE = "__tab_presentation_none__";

// Resource kinds where a persisted title is a good first-frame fallback while
// the live data loads. Flow/unknown/attachment always use their type label.
const PENDING_RESOURCE_KEYS: ReadonlySet<TabLabelKey> = new Set<TabLabelKey>([
  "issue",
  "project",
  "autopilot",
  "agent",
  "member",
  "squad",
  "skill",
  "machine",
  "runtime",
]);

function useTabEntityData(subject: TabSubject, wsId: string, fetch: boolean): TabEntityData {
  const { t: chatT } = useT("chat");

  // Read both inbox lists cache-only; the archived view keeps its own list, so
  // an archived selection has to resolve against the archived cache — the same
  // list the InboxPage populates when `?view=archived` is active.
  const inboxList = useQuery({ ...inboxListOptions(wsId), enabled: false }).data;
  const inboxFilters = useInboxFilters(wsId);
  const archivedPages = useInfiniteQuery({ ...archivedInboxPagesOptions(wsId, inboxFilters), enabled: false }).data;
  const archivedLookup = useQuery({
    ...archivedInboxLookupOptions(wsId, subject.kind === "inbox" ? subject.selectedKey ?? "" : ""),
    enabled: false,
  }).data;
  const archivedInboxList = useMemo(() => [
    ...(archivedPages?.pages.flatMap((page) => page.items) ?? []),
    ...(archivedLookup?.items ?? []),
  ], [archivedPages, archivedLookup]);
  const activeInboxList =
    subject.kind === "inbox" && subject.archived ? archivedInboxList : inboxList;
  const inboxItem =
    subject.kind === "inbox" && subject.selectedKey
      ? (activeInboxList?.find(
          (i) => (i.issue_id ?? i.id) === subject.selectedKey,
        ) ?? null)
      : null;

  // One issue query serves both a direct issue tab and an inbox-selected issue.
  //
  // An inbox selection resolves straight from `selectedKey`: it IS the key the
  // inbox groups by (`issue_id ?? id`), so for an issue-backed notification it
  // already IS the issue id, and the hop through the list row was redundant.
  // Going direct means a restored tab resolves from the issue cache alone,
  // instead of silently depending on someone having fetched the whole Inbox
  // first (MUL-6967). A key that is an issue-less notification's own id simply
  // matches no issue, and falls through to the item branch below.
  const issueId =
    subject.kind === "issue"
      ? subject.id
      : subject.kind === "inbox"
        ? (subject.selectedKey ?? "")
        : "";
  // An inbox key can name an issue-less notification. Only fetch known
  // issue-backed rows; unknown selections can still resolve from cache.
  // The canonical query seeds/subscribes to the UUID entry even when the URL
  // uses an identifier, so background tabs receive realtime patches too.
  const { issue } = useCanonicalIssue(wsId, issueId, {
    enabled: fetch && (subject.kind === "issue" || !!inboxItem?.issue_id),
  });

  const project = useQuery({
    ...projectDetailOptions(wsId, subject.kind === "project" ? subject.id : NONE),
    enabled: false,
  }).data;
  const autopilot = useQuery({
    ...autopilotDetailOptions(
      wsId,
      subject.kind === "autopilot" ? subject.id : NONE,
    ),
    enabled: false,
  }).data;
  const skill = useQuery({
    ...skillDetailOptions(wsId, subject.kind === "skill" ? subject.id : NONE),
    enabled: false,
  }).data;

  const agents = useQuery({ ...agentListOptions(wsId), enabled: false }).data;
  const members = useQuery({ ...memberListOptions(wsId), enabled: false }).data;
  const squads = useQuery({ ...squadListOptions(wsId), enabled: false }).data;
  const runtimes = useQuery({ ...runtimeListOptions(wsId), enabled: false }).data;
  const chatSession = useQuery({ ...chatSessionOptions(wsId, subject.kind === "chat" ? subject.sessionId ?? "" : ""), enabled: false }).data;

  const data: TabEntityData = {};
  switch (subject.kind) {
    case "issue":
      if (issue) {
        data.issue = {
          identifier: issue.identifier,
          title: issue.title,
          status: issue.status,
          status_category: issue.status_category,
        };
      }
      break;
    case "project":
      if (project) data.project = { icon: project.icon, title: project.title };
      break;
    case "autopilot":
      if (autopilot) data.autopilot = { title: autopilot.autopilot.title };
      break;
    case "skill":
      if (skill) data.skill = { name: skill.name };
      break;
    case "actor": {
      const name =
        subject.actorType === "agent"
          ? agents?.find((a) => a.id === subject.id)?.name
          : subject.actorType === "member"
            ? members?.find((m) => m.user_id === subject.id)?.name
            : squads?.find((s) => s.id === subject.id)?.name;
      if (name) data.actorName = name;
      break;
    }
    case "machine": {
      const rt = runtimes?.find((r) => r.id === subject.machineId);
      if (rt) data.machine = { name: runtimeDisplayName(rt) };
      break;
    }
    case "runtime": {
      const rt = runtimes?.find((r) => r.id === subject.runtimeId);
      if (rt) data.runtime = { name: runtimeDisplayName(rt) };
      break;
    }
    case "chat":
      if (subject.sessionId) {
        const s = chatSession;
        if (s) data.chatSessionTitle = s.title?.trim() || chatT(($) => $.window.untitled);
      }
      break;
    case "inbox":
      if (issue) {
        // Issue-backed selection — answered by the issue cache alone.
        data.inboxSelection = {
          kind: "issue",
          identifier: issue.identifier,
          title: issue.title,
        };
      } else if (inboxItem && !inboxItem.issue_id) {
        // An issue-less notification (autopilot paused, quota exceeded, …)
        // carries its title only on the inbox row, so this branch is the one
        // case that still needs the list cache.
        data.inboxSelection = {
          kind: "item",
          title: getInboxDisplayTitle(inboxItem),
        };
      }
      break;
  }
  return data;
}

/**
 * Localize a title spec, preferring a persisted fallback while pending.
 *
 * `pending` is for identities the spec cannot report as pending on its own. A
 * resource tab that has not loaded resolves to its type label, which
 * `PENDING_RESOURCE_KEYS` recognizes; a CONTAINER tab with an unresolved
 * selection resolves to the container's own nav label, which is
 * indistinguishable from a container with nothing selected — so the caller,
 * which still has the subject, decides.
 */
function useTabTitle(
  spec: TabTitleSpec,
  fallbackTitle?: string,
  pending = false,
): string {
  const { t: layoutT } = useT("layout");
  const persisted = fallbackTitle?.trim();
  switch (spec.kind) {
    case "text":
      return spec.text;
    case "nav":
      return pending && persisted
        ? persisted
        : layoutT(($) => $.nav[spec.navKey]);
    case "tab": {
      if (pending || PENDING_RESOURCE_KEYS.has(spec.tabKey)) {
        if (persisted) return persisted;
      }
      return layoutT(($) => $.tab[spec.tabKey]);
    }
  }
}

export interface TabPresentationResult {
  visual: TabVisual;
  title: string;
  /** False while a resource title is only a type label or persisted fallback. */
  titleResolved: boolean;
}

/**
 * Resolve a tab URL into its live leading visual and title.
 *
 * `fallbackTitle` (the tab's persisted title) is used only as a first-frame
 * fallback for a still-loading resource; once the cache resolves, the live
 * presentation wins.
 */
export function useTabPresentation(
  url: string,
  fallbackTitle?: string,
  { fetch = false }: { fetch?: boolean } = {},
): TabPresentationResult {
  const subject = useMemo(() => parseTabSubject(url), [url]);
  const ws = useCurrentWorkspace();
  const wsId = ws?.id ?? "";
  const data = useTabEntityData(subject, wsId, fetch);
  const statuses = useQuery({
    ...issueStatusListOptions(wsId),
    enabled: fetch && !!wsId && subject.kind === "issue",
  }).data;
  const catalog = useMemo(() => buildIssueStatusCatalog(statuses), [statuses]);
  const { visual, title: titleSpec } = resolveTabPresentation(subject, data);
  // A selected notification or chat session not resolved from cache yet is
  // pending in exactly the sense a not-yet-loaded issue is — keep the tab's
  // persisted title instead of collapsing it to the bare container label.
  const selectionPending =
    (subject.kind === "inbox" && !!subject.selectedKey && !data.inboxSelection) ||
    (subject.kind === "chat" && !!subject.sessionId && !data.chatSessionTitle);
  const title = useTabTitle(titleSpec, fallbackTitle, selectionPending);

  // The actor avatar resolves through workspace directory queries and throws
  // if rendered before the workspace exists. Until it does, show a type icon.
  const safeVisual: TabVisual =
    visual.kind === "actor" && !wsId
      ? {
          kind: "icon",
          icon:
            visual.actorType === "squad"
              ? "Users"
              : visual.actorType === "member"
                ? "CircleUser"
                : "Bot",
        }
      : visual;

  return {
    visual: safeVisual.kind === "issue-status" && safeVisual.status && catalog.entryOf(safeVisual.status)?.is_system === false
      ? {
          ...safeVisual,
          category: safeVisual.category ?? catalog.categoryOf(safeVisual.status),
          color: catalog.colorOf(safeVisual.status),
          icon: catalog.iconOf(safeVisual.status),
        }
      : safeVisual,
    title,
    titleResolved: !selectionPending &&
      (titleSpec.kind !== "tab" || !PENDING_RESOURCE_KEYS.has(titleSpec.tabKey)),
  };
}

/**
 * Render a tab's leading visual into a fixed 16×16 slot so the tab never
 * reflows when the visual resolves from a type fallback to the real identity.
 * Shared by the desktop tab bar (and reusable by any resource row that wants
 * the same identity rules).
 */
export function ResourceLeadingVisual({
  visual,
  className,
}: {
  visual: TabVisual;
  className?: string;
}) {
  let inner: React.ReactNode;
  switch (visual.kind) {
    case "icon": {
      const Icon = ROUTE_ICON_COMPONENTS[visual.icon];
      inner = <Icon className="size-3.5" />;
      break;
    }
    case "issue-status":
      inner = visual.status === null ? (
        <Skeleton aria-hidden className="size-3 rounded-sm" />
      ) : (
        <StatusIcon
          status={visual.status}
          category={visual.category}
          color={visual.color}
          icon={visual.icon}
          className="size-3.5"
        />
      );
      break;
    case "project-icon":
      inner = <ProjectIcon project={{ icon: visual.icon }} size="sm" />;
      break;
    case "actor":
      inner = (
        <ActorAvatar
          actorType={visual.actorType}
          actorId={visual.id}
          size="xs"
          profileLink={false}
        />
      );
      break;
  }
  return (
    <span
      className={cn(
        "flex size-4 shrink-0 items-center justify-center",
        className,
      )}
    >
      {inner}
    </span>
  );
}
