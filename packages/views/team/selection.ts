/**
 * Team page selection lives in the URL so every object is deep-linkable and a
 * desktop tab knows what it shows: `?agent=` | `?squad=` | `?member=` |
 * `?channel=`, plus `tab=` and `thread=` (the thread under one message of the
 * conversation shown). An agent also takes `session=`, an earlier
 * conversation with it.
 */

export type TeamTab = "chat" | "work" | "setup";

export type TeamSelection =
  | { kind: "none" }
  | { kind: "agent"; id: string; tab: TeamTab; sessionId: string | null; threadId: string | null }
  | { kind: "squad"; id: string; tab: TeamTab }
  | { kind: "member"; id: string; tab: TeamTab; threadId: string | null }
  | { kind: "channel"; id: string; tab: TeamTab; threadId: string | null };

export type TeamSelectionKind = Exclude<TeamSelection["kind"], "none">;

/** Tabs each kind offers, in display order; the first is the default. */
export const TEAM_TABS: Record<TeamSelectionKind, TeamTab[]> = {
  agent: ["chat", "work", "setup"],
  squad: ["work", "setup"],
  member: ["chat", "work"],
  channel: ["chat"],
};

function tabFor(kind: TeamSelectionKind, raw: string | null): TeamTab {
  const tabs = TEAM_TABS[kind];
  return tabs.find((t) => t === raw) ?? tabs[0]!;
}

export function parseTeamSelection(params: URLSearchParams): TeamSelection {
  const tab = params.get("tab");
  const threadId = params.get("thread") || null;
  const agent = params.get("agent");
  if (agent) {
    return {
      kind: "agent",
      id: agent,
      tab: tabFor("agent", tab),
      sessionId: params.get("session") || null,
      threadId,
    };
  }
  const squad = params.get("squad");
  if (squad) return { kind: "squad", id: squad, tab: tabFor("squad", tab) };
  const member = params.get("member");
  if (member) return { kind: "member", id: member, tab: tabFor("member", tab), threadId };
  const channel = params.get("channel");
  if (channel) return { kind: "channel", id: channel, tab: "chat", threadId };
  return { kind: "none" };
}

/** Build the Team URL for a selection; the default tab is omitted. */
export function teamHref(
  base: string,
  target:
    | {
        kind: TeamSelectionKind;
        id: string;
        tab?: TeamTab;
        sessionId?: string | null;
        threadId?: string | null;
      }
    | { kind: "none" },
): string {
  if (target.kind === "none") return base;
  const params = new URLSearchParams();
  params.set(target.kind, target.id);
  if (target.tab && target.tab !== TEAM_TABS[target.kind][0]) params.set("tab", target.tab);
  if (target.kind === "agent" && target.sessionId) params.set("session", target.sessionId);
  if (target.kind !== "squad" && target.threadId) params.set("thread", target.threadId);
  return `${base}?${params.toString()}`;
}
