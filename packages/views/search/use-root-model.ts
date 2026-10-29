"use client";

import { useMemo } from "react";
import type { CommandBarContext } from "@multica/core/command-bar";
import { WORKSPACE_PAGES, type WorkspacePageKey } from "@multica/core/paths";
import { partitionAggregatedSearchResults } from "@multica/core/search/cancelled-rank";
import type { SearchIssueResult, SearchProjectResult } from "@multica/core/types";
import { useT } from "../i18n";
import { useAgentCommandGroup, useAutopilotCommandGroup, useChatCommandGroup, useInboxCommandGroup, useProjectCommandGroup } from "./commands/context-commands";
import { useGlobalCommands } from "./commands/global-commands";
import { useIssueCommandGroup } from "./commands/issue-commands";
import { detectInstruction, type NamedActor } from "./delegate/detect-instruction";
import { useDelegateAttachments, useDelegateCandidates, type DelegateAttachments } from "./delegate/use-delegate";
import { parseScope, scoreRow, scoreText, type SearchScope } from "./matching";
import type { DelegateActor, IssueTarget, PaletteCommand, PaletteCommandGroup } from "./types";
import type { AgentEntry } from "./use-actor-directory";
import { useObjectSearch, type ObjectHits, type Scored } from "./use-object-search";
import { useRecentItems, type RecentItem } from "./use-recent-items";
import { useServerSearch, type SearchResults } from "./use-server-search";

// The palette's Pages group is generated from WORKSPACE_PAGES, the same
// registry the sidebar nav and the desktop tab bar read. It used to be a
// hand-written list, which silently went stale every time a page was added:
// Chat, Autopilot, Squads and Analytics shipped in the sidebar but were
// unreachable from the palette (MUL-6272). Deriving the list means a new
// workspace page is in the palette the moment it is in the registry.
//
// Page keys double as WorkspacePaths method names, so `p[key]()` resolves the
// destination against the current workspace slug at render time. Every
// WorkspacePageKey must therefore stay a parameterless path builder.

// Extra query aliases per page, on top of the localized label. Declared as a
// total Record so adding a workspace page is a compile error until its
// keywords are filled in.
const PAGE_KEYWORDS: Record<WorkspacePageKey, string[]> = {
  inbox: ["inbox", "notifications", "收件箱", "通知"],
  chat: ["chat", "messages", "conversation", "聊天", "消息", "对话"],
  myIssues: ["my", "issues", "assigned", "mine", "我的", "任务"],
  issues: ["issues", "tasks", "bugs", "任务"],
  projects: ["projects", "kanban", "项目"],
  autopilots: ["autopilot", "autopilots", "automation", "schedule", "cron", "webhook", "自动化", "定时"],
  agents: ["agents", "bots", "ai", "智能体"],
  squads: ["squads", "teams", "小队", "团队"],
  usage: ["usage", "analytics", "stats", "metrics", "统计", "分析", "用量"],
  runtimes: ["runtimes", "environments", "machines", "运行时"],
  skills: ["skills", "library", "技能"],
  settings: ["settings", "config", "preferences", "设置", "配置"],
};

const NAV_PAGE_KEYS = Object.keys(WORKSPACE_PAGES) as WorkspacePageKey[];

export interface NavPage {
  key: WorkspacePageKey;
  label: string;
  keywords: string[];
}

export type BestMatch =
  | { type: "agent"; entry: AgentEntry; score: number }
  | { type: "squad"; entry: ObjectHits["squads"][number]["item"]; score: number }
  | { type: "member"; member: ObjectHits["members"][number]["item"]; score: number }
  | { type: "project"; project: SearchProjectResult; score: number };

export interface DelegateModel {
  prompt: string;
  actor: DelegateActor;
  agentEntry: AgentEntry | null;
  attachments: DelegateAttachments;
}

export interface RootModel {
  scope: SearchScope;
  text: string;
  /** Lower-cased `text`. */
  q: string;
  contextGroups: (PaletteCommandGroup & { score: number })[];
  commands: Scored<PaletteCommand>[];
  pages: Scored<NavPage>[];
  objects: ObjectHits;
  server: { results: SearchResults; isLoading: boolean; stale: boolean };
  partitioned: ReturnType<typeof partitionAggregatedSearchResults>;
  liveIssueScore: number;
  liveProjectScore: number;
  bestMatch: BestMatch | null;
  recent: RecentItem[];
  newCommands: PaletteCommand[];
  delegate: DelegateModel | null;
  /** The issue or selection that best-match actions such as "Assign … to" act on. */
  primaryIssueTarget: IssueTarget | null;
  /** Context a delegated issue is created with. */
  attachments: DelegateAttachments;
  hasResults: boolean;
}

// Server rows answer a query the client cannot re-score exactly (they may
// have matched on a description or comment). A title hit scores like any
// other row; a snippet-only hit ranks below every client-side match.
const SERVER_ONLY_SCORE = 10;

export function issueScore(issue: SearchIssueResult, q: string): number {
  if (issue.identifier.toLowerCase() === q) return 100;
  return Math.max(scoreText(issue.title, q), SERVER_ONLY_SCORE);
}

export function projectScore(project: SearchProjectResult, q: string): number {
  return Math.max(scoreText(project.title, q), SERVER_ONLY_SCORE);
}

function maxScore(entries: readonly { score: number }[]) {
  return entries.reduce((max, entry) => Math.max(max, entry.score), 0);
}

export function useRootModel(
  rawQuery: string,
  contexts: readonly CommandBarContext[],
  options: { active: boolean; dismissedDelegateFor: string | null },
): RootModel {
  const { t: tNav } = useT("layout");
  const { scope, text } = parseScope(rawQuery);
  const q = text.toLowerCase();
  const active = options.active;

  // Context lookups by kind; the store keeps newest registration first.
  const find = <K extends CommandBarContext["kind"]>(kind: K) =>
    contexts.find((c): c is Extract<CommandBarContext, { kind: K }> => c.kind === kind) ?? null;
  const issueContext = find("issue");
  const issuesContext = find("issues");
  const singleTarget = useMemo<IssueTarget | null>(
    () => (issueContext ? { kind: "single", issue: issueContext.issue } : null),
    [issueContext],
  );
  const batchTarget = useMemo<IssueTarget | null>(
    () =>
      issuesContext
        ? { kind: "batch", issues: issuesContext.issues, clearSelection: issuesContext.clearSelection }
        : null,
    [issuesContext],
  );

  const singleGroup = useIssueCommandGroup(singleTarget, issueContext?.onDeletedFallbackPath);
  const batchGroup = useIssueCommandGroup(batchTarget);
  const projectGroup = useProjectCommandGroup(find("project"));
  const agentGroup = useAgentCommandGroup(find("agent"));
  const autopilotGroup = useAutopilotCommandGroup(find("autopilot"));
  const inboxGroup = useInboxCommandGroup(find("inbox"));
  const chatGroup = useChatCommandGroup(find("chat"));
  const globalCommands = useGlobalCommands(contexts);

  const searching = active && scope !== "commands" && scope !== "people" ? text : "";
  const server = useServerSearch(searching, { projects: scope === "all" });
  const objects = useObjectSearch(scope === "issues" || scope === "commands" ? "" : q, active);
  const recent = useRecentItems(active && !rawQuery.trim());
  const delegateCandidates = useDelegateCandidates(active);
  const attachments = useDelegateAttachments(contexts);

  const navPages = useMemo<NavPage[]>(
    () =>
      NAV_PAGE_KEYS.map((key) => ({
        key,
        label: tNav(($) => $.nav[WORKSPACE_PAGES[key].navKey]),
        keywords: PAGE_KEYWORDS[key],
      })),
    [tNav],
  );

  return useMemo<RootModel>(() => {
    const groupByKind: Record<string, PaletteCommandGroup | null> = {
      issue: singleGroup,
      issues: batchGroup,
      project: projectGroup,
      agent: agentGroup,
      autopilot: autopilotGroup,
      inbox: inboxGroup,
      chat: chatGroup,
    };
    const orderedGroups = contexts
      .map((c) => groupByKind[c.kind])
      .filter((group, index, all): group is PaletteCommandGroup => !!group && all.indexOf(group) === index);

    const commandsAllowed = scope === "all" || scope === "commands";
    let contextGroups: RootModel["contextGroups"] = [];
    let commands: Scored<PaletteCommand>[] = [];
    let pages: Scored<NavPage>[] = [];
    if (!q) {
      if (scope === "all") {
        contextGroups = orderedGroups
          .map((group) => ({ ...group, commands: group.commands.filter((c) => c.primary), score: 0 }))
          .filter((group) => group.commands.length > 0);
      } else if (scope === "commands") {
        contextGroups = orderedGroups.map((group) => ({ ...group, score: 0 }));
        commands = globalCommands.map((command) => ({ item: command, score: 0 }));
      }
    } else if (commandsAllowed) {
      const rank = (list: PaletteCommand[]) =>
        list
          .map((command) => ({ item: command, score: scoreRow(command.label, command.keywords ?? [], q) }))
          .filter((entry) => entry.score > 0)
          .sort((a, b) => b.score - a.score);
      contextGroups = orderedGroups
        .map((group) => {
          const ranked = rank(group.commands);
          return { ...group, commands: ranked.map((r) => r.item), score: maxScore(ranked) };
        })
        .filter((group) => group.commands.length > 0);
      commands = rank(globalCommands);
      pages = navPages
        .map((page) => ({ item: page, score: scoreRow(page.label, page.keywords, q) }))
        .filter((entry) => entry.score > 0)
        .sort((a, b) => b.score - a.score);
    }

    const { results, isLoading, stale } = server;
    // The partition describes these rows, so it keys off the query they
    // answer — not what the user has typed since.
    const partitioned = partitionAggregatedSearchResults({
      issues: results.issues,
      projects: scope === "all" ? results.projects : [],
      query: results.query,
    });
    const resultsQ = results.query.toLowerCase();
    const liveIssueScore = partitioned.liveIssues.reduce((max, issue) => Math.max(max, issueScore(issue, resultsQ)), 0);
    const liveProjectScore = partitioned.liveProjects.reduce(
      (max, project) => Math.max(max, projectScore(project, resultsQ)),
      0,
    );

    const people = scope === "all" || scope === "people";
    const visibleObjects: ObjectHits = {
      agents: people ? objects.agents : [],
      squads: people ? objects.squads : [],
      members: people ? objects.members : [],
      autopilots: scope === "all" ? objects.autopilots : [],
      chats: scope === "all" ? objects.chats : [],
      skills: scope === "all" ? objects.skills : [],
      workspaces: scope === "all" ? objects.workspaces : [],
    };

    // Best match: a top hit worth acting on directly ("Assign MUL-1 to
    // Lambda", "Chat with Lambda") is lifted above everything, actions under
    // it. Only prefix-or-better hits qualify; a substring hit is a guess.
    let bestMatch: BestMatch | null = null;
    if (q && scope === "all") {
      const candidates: BestMatch[] = [];
      const agent = visibleObjects.agents[0];
      if (agent) candidates.push({ type: "agent", entry: agent.item, score: agent.score });
      const squad = visibleObjects.squads[0];
      if (squad) candidates.push({ type: "squad", entry: squad.item, score: squad.score });
      const member = visibleObjects.members[0];
      if (member) candidates.push({ type: "member", member: member.item, score: member.score });
      if (!stale) {
        const project = partitioned.liveProjects.find((p) => scoreText(p.title, resultsQ) >= 80);
        if (project) candidates.push({ type: "project", project, score: scoreText(project.title, resultsQ) });
      }
      const top = candidates.sort((a, b) => b.score - a.score)[0];
      if (top && top.score >= 80 && top.score >= liveIssueScore) bestMatch = top;
    }

    const objectCount =
      visibleObjects.agents.length +
      visibleObjects.squads.length +
      visibleObjects.members.length +
      visibleObjects.autopilots.length +
      visibleObjects.chats.length +
      visibleObjects.skills.length +
      visibleObjects.workspaces.length;
    const hasResults =
      objectCount > 0 ||
      results.issues.length > 0 ||
      results.projects.length > 0 ||
      pages.length > 0 ||
      commands.length > 0 ||
      contextGroups.length > 0;

    let delegate: DelegateModel | null = null;
    if (q && scope === "all" && options.dismissedDelegateFor !== rawQuery) {
      const named: NamedActor[] = delegateCandidates.actors.map((actor) => ({
        type: actor.type,
        id: actor.id,
        name: actor.name,
      }));
      const detected = detectInstruction(text, named, {
        hasResults: objectCount > 0 || results.issues.length > 0 || results.projects.length > 0,
        resultsSettled: !isLoading && !stale,
      });
      const actor = detected?.actor
        ? (delegateCandidates.actors.find((a) => a.type === detected.actor!.type && a.id === detected.actor!.id) ?? null)
        : delegateCandidates.fallback;
      if (detected && actor) {
        delegate = {
          prompt: text,
          actor,
          agentEntry:
            actor.type === "agent"
              ? (delegateCandidates.agentEntries.find((entry) => entry.agent.id === actor.id) ?? null)
              : null,
          attachments,
        };
      }
    }

    const primaryIssueTarget =
      contexts
        .map((c) => (c.kind === "issue" ? singleTarget : c.kind === "issues" ? batchTarget : null))
        .find((target): target is IssueTarget => !!target) ?? null;

    return {
      scope,
      text,
      q,
      contextGroups,
      commands,
      pages,
      objects: visibleObjects,
      server,
      partitioned,
      liveIssueScore,
      liveProjectScore,
      bestMatch,
      recent,
      newCommands: globalCommands.filter((command) => command.primary),
      delegate,
      primaryIssueTarget,
      attachments,
      hasResults,
    };
  }, [
    agentGroup,
    attachments,
    autopilotGroup,
    batchGroup,
    batchTarget,
    chatGroup,
    contexts,
    delegateCandidates,
    globalCommands,
    inboxGroup,
    navPages,
    objects,
    options.dismissedDelegateFor,
    projectGroup,
    q,
    rawQuery,
    recent,
    scope,
    server,
    singleGroup,
    singleTarget,
    text,
  ]);
}
