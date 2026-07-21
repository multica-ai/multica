"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
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
import { issueDetailOptions } from "@multica/core/issues/queries";
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
import { chatSessionsOptions } from "@multica/core/chat/queries";
import {
  inboxListOptions,
  archivedInboxListOptions,
} from "@multica/core/inbox/queries";
import { cn } from "@multica/ui/lib/utils";
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
 * Cache-only reads: every query in `useTabEntityData` is `enabled: false`. It
 * observes whatever the pages/directory already loaded and re-renders when that
 * data changes, so an open tab's icon/title stay in sync (project renamed,
 * issue status changed, chat session retitled) without amplifying requests. A
 * resource that has not loaded yet renders a stable type fallback until its
 * page fills the cache.
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

/** Gather cached entity data for a subject. All reads are cache-only. */
function useTabEntityData(subject: TabSubject, wsId: string): TabEntityData {
  const { t: chatT } = useT("chat");

  // Read both inbox lists cache-only; the archived view keeps its own list, so
  // an archived selection has to resolve against the archived cache — the same
  // list the InboxPage populates when `?view=archived` is active.
  const inboxList = useQuery({ ...inboxListOptions(wsId), enabled: false }).data;
  const archivedInboxList = useQuery({
    ...archivedInboxListOptions(wsId),
    enabled: false,
  }).data;
  const activeInboxList =
    subject.kind === "inbox" && subject.archived ? archivedInboxList : inboxList;
  const inboxItem =
    subject.kind === "inbox" && subject.selectedKey
      ? (activeInboxList?.find(
          (i) => (i.issue_id ?? i.id) === subject.selectedKey,
        ) ?? null)
      : null;

  // One issue query serves both a direct issue tab and an inbox-selected issue.
  const issueId =
    subject.kind === "issue"
      ? subject.id
      : (inboxItem?.issue_id ?? "");
  const issue = useQuery({
    ...issueDetailOptions(wsId, issueId || NONE),
    enabled: false,
  }).data;

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
  const sessions = useQuery({ ...chatSessionsOptions(wsId), enabled: false }).data;

  const data: TabEntityData = {};
  switch (subject.kind) {
    case "issue":
      if (issue) {
        data.issue = {
          identifier: issue.identifier,
          title: issue.title,
          status: issue.status,
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
        const s = sessions?.find((x) => x.id === subject.sessionId);
        if (s) data.chatSessionTitle = s.title?.trim() || chatT(($) => $.window.untitled);
      }
      break;
    case "inbox":
      if (inboxItem) {
        if (inboxItem.issue_id && issue) {
          data.inboxSelection = {
            kind: "issue",
            identifier: issue.identifier,
            title: issue.title,
          };
        } else if (!inboxItem.issue_id) {
          data.inboxSelection = {
            kind: "item",
            title: getInboxDisplayTitle(inboxItem),
          };
        }
      }
      break;
  }
  return data;
}

/** Localize a title spec, preferring a persisted fallback while pending. */
function useTabTitle(spec: TabTitleSpec, fallbackTitle?: string): string {
  const { t: layoutT } = useT("layout");
  switch (spec.kind) {
    case "text":
      return spec.text;
    case "nav":
      return layoutT(($) => $.nav[spec.navKey]);
    case "tab": {
      if (PENDING_RESOURCE_KEYS.has(spec.tabKey)) {
        const clean = fallbackTitle?.trim();
        if (clean) return clean;
      }
      return layoutT(($) => $.tab[spec.tabKey]);
    }
  }
}

export interface TabPresentationResult {
  visual: TabVisual;
  title: string;
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
): TabPresentationResult {
  const subject = useMemo(() => parseTabSubject(url), [url]);
  const ws = useCurrentWorkspace();
  const wsId = ws?.id ?? "";
  const data = useTabEntityData(subject, wsId);
  const { visual, title: titleSpec } = resolveTabPresentation(subject, data);
  const title = useTabTitle(titleSpec, fallbackTitle);

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

  return { visual: safeVisual, title };
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
      // A null status (loading) renders StatusIcon's neutral fallback glyph.
      inner = <StatusIcon status={visual.status ?? ""} className="size-3.5" />;
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
