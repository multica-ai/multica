"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { docTreeOptions } from "@multica/core/docs";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { BUILTIN_VIEW_KEYS, type BuiltinViewKey } from "@multica/core/issue-views/sidebar-views";
import { skillListOptions, workspaceListOptions } from "@multica/core/workspace/queries";
import type { IssueView } from "@multica/core/api/schemas";
import type {
  Autopilot,
  ChatSession,
  DocSummary,
  MemberWithUser,
  SkillSummary,
  Workspace,
} from "@multica/core/types";
import { useBuiltinViewLabel } from "../issue-views/builtin-views";
import { scoreRow, scoreText } from "./matching";
import { useActorDirectory, type AgentEntry, type SquadEntry } from "./use-actor-directory";

export interface Scored<T> {
  item: T;
  score: number;
}

/** A built-in view, or a saved view the user can open. */
export type PaletteView =
  | { kind: "builtin"; key: BuiltinViewKey; name: string }
  | { kind: "view"; view: IssueView; name: string };

export interface ObjectHits {
  agents: Scored<AgentEntry>[];
  squads: Scored<SquadEntry>[];
  members: Scored<MemberWithUser>[];
  views: Scored<PaletteView>[];
  autopilots: Scored<Autopilot>[];
  chats: Scored<ChatSession>[];
  skills: Scored<SkillSummary>[];
  docs: Scored<PaletteDoc>[];
  workspaces: Scored<Workspace>[];
}

/** A doc and the folder it sits in, shown as the row's detail. */
export interface PaletteDoc {
  doc: DocSummary;
  folder: string | null;
}

const EMPTY_AUTOPILOTS: Autopilot[] = [];
const EMPTY_CHATS: ChatSession[] = [];
const EMPTY_SKILLS: SkillSummary[] = [];
const EMPTY_DOC_TREE = { folders: [], docs: [] };
const EMPTY_VIEWS: IssueView[] = [];
const EMPTY_WORKSPACES: Workspace[] = [];

// Hidden aliases for the Workspaces group, prefix-matched like page keywords:
// typing "workspace" lists every workspace the user can switch to.
const WORKSPACE_KEYWORDS = ["switch", "workspace"];
const ALL_MEMBERS_WORDS = ["members", "people", "users", "team"];
// Typing "views" lists every view under the Views page row.
const ALL_VIEWS_WORDS = ["views", "视图"];

function rank<T>(items: readonly T[], score: (item: T) => number, limit: number): Scored<T>[] {
  return items
    .map((item) => ({ item, score: score(item) }))
    .filter((entry) => entry.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit);
}

function memberScore(member: MemberWithUser, query: string): number {
  const byName = scoreText(member.name, query);
  if (byName > 0) return byName;
  if (member.email.toLowerCase().includes(query)) return 40;
  if (query.length >= 3 && member.role.startsWith(query)) return 25;
  if (query.length >= 3 && ALL_MEMBERS_WORDS.some((word) => word.startsWith(query))) return 20;
  return 0;
}

function viewScore(view: PaletteView, query: string): number {
  const byName = scoreText(view.name, query);
  if (byName > 0) return byName;
  if (query.length >= 2 && ALL_VIEWS_WORDS.some((word) => word.startsWith(query))) return 20;
  return 0;
}

/**
 * Client-side search over the workspace lists the app already caches. Lists
 * the palette does not otherwise need (views, autopilots, chats, skills) load
 * only once the user types.
 */
export function useObjectSearch(query: string, open: boolean): ObjectHits & { directory: ReturnType<typeof useActorDirectory> } {
  const wsId = useWorkspaceId();
  const directory = useActorDirectory(open);
  const typing = open && query.length > 0;
  const builtinLabel = useBuiltinViewLabel();
  const { data: savedViews = EMPTY_VIEWS } = useQuery({ ...issueViewEverywhereOptions(wsId), enabled: typing && !!wsId });
  const { data: autopilots = EMPTY_AUTOPILOTS } = useQuery({ ...autopilotListOptions(wsId), enabled: typing });
  const { data: chats = EMPTY_CHATS } = useQuery({ ...chatSessionsOptions(wsId), enabled: typing });
  const { data: skills = EMPTY_SKILLS } = useQuery({ ...skillListOptions(wsId), enabled: typing });
  const { data: docTree = EMPTY_DOC_TREE } = useQuery({ ...docTreeOptions(wsId), enabled: typing && !!wsId });
  const docs = useMemo<PaletteDoc[]>(() => {
    const folders = new Map(docTree.folders.map((f) => [f.id, f.title]));
    return docTree.docs
      .filter((doc) => doc.title.trim() !== "")
      .map((doc) => ({ doc, folder: doc.folderId ? (folders.get(doc.folderId) ?? null) : null }));
  }, [docTree]);
  // Account-level cache entry the sidebar's workspace switcher already
  // holds, so the Workspaces group adds no request of its own.
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());

  const views = useMemo<PaletteView[]>(
    () => [
      ...BUILTIN_VIEW_KEYS.map((key) => ({ kind: "builtin" as const, key, name: builtinLabel(key) })),
      ...savedViews.map((view) => ({ kind: "view" as const, view, name: view.name })),
    ],
    [builtinLabel, savedViews],
  );

  const hits = useMemo<ObjectHits>(() => {
    if (!query) {
      return { agents: [], squads: [], members: [], views: [], autopilots: [], chats: [], skills: [], docs: [], workspaces: [] };
    }
    return {
      agents: rank(directory.agents, (e) => scoreText(e.agent.name, query), 8),
      squads: rank(directory.squads, (e) => scoreText(e.squad.name, query), 5),
      members: rank(directory.members, (m) => memberScore(m, query), 10),
      views: rank(views, (v) => viewScore(v, query), 8),
      autopilots: rank(
        autopilots.filter((a) => a.status !== "archived"),
        (a) => scoreText(a.title, query),
        5,
      ),
      chats: rank(
        chats.filter((c) => c.status !== "archived"),
        (c) => scoreText(c.title, query),
        5,
      ),
      skills: rank(skills, (s) => scoreText(s.name, query), 5),
      docs: rank(docs, (d) => scoreText(d.doc.title, query), 6),
      // The current workspace is never a switch target, so a single-workspace
      // user never sees the group. Ties keep the server's created_at order,
      // the same order as the sidebar switcher.
      workspaces: rank(
        workspaces.filter((ws) => ws.id !== wsId),
        (ws) => Math.max(scoreRow(ws.name, WORKSPACE_KEYWORDS, query), ws.slug.toLowerCase().includes(query) ? 40 : 0),
        50,
      ),
    };
  }, [autopilots, chats, directory, docs, query, skills, views, workspaces, wsId]);

  return { ...hits, directory };
}
