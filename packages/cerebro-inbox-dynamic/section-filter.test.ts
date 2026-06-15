import { describe, it, expect } from "vitest";
import type { InboxItem, Channel } from "@multica/core/types";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import {
  entryIsUnread,
  entryProjectId,
  entryIsMentioned,
  entryIsRunning,
  entryMatchesSection,
  selectSectionEntries,
  sectionFilters,
  sortEntries,
  matchesViewFilter,
  entryIsMuted,
  groupSectionEntries,
  type DynInboxEntry,
  type SectionFilterContext,
  type SectionGroupContext,
} from "./section-filter";
import type { InboxActionCategory } from "@multica/cerebro-inbox";
import {
  deleteUserInboxPreset,
  dedupeLayoutIds,
  isValidLayout,
  makeId,
  makeUserInboxPresetsBlob,
  operatorPreset,
  readUserInboxPresets,
  sectionLabel,
  upsertUserInboxPreset,
  type InboxLayout,
} from "./layout";

function notif(over: Partial<InboxItem> = {}): InboxItem {
  return {
    id: over.id ?? makeId("n"),
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "agent",
    actor_id: "a1",
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "iss1",
    project_id: null,
    title: "t",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-06-11T10:00:00Z",
    details: null,
    ...over,
  } as InboxItem;
}

function notifEntry(time: number, over: Partial<InboxItem> = {}): DynInboxEntry {
  const item = notif(over);
  return { kind: "notif", id: item.id, time, item };
}

function channelEntry(time: number, over: Partial<Channel> = {}): DynInboxEntry {
  const channel = { id: makeId("c"), unread_count: 0, project_id: null, ...over } as Channel;
  return { kind: "channel", id: channel.id, time, channel };
}

const ctx: SectionFilterContext = {
  action: {
    userId: "me",
    issueRunStates: new Map(),
    subIssueRunStates: new Map(),
    chatRunStates: new Map(),
    mentionedChannels: new Set(),
    wakeupIssueIds: new Set(),
  } as InboxActionContext,
  matchesPins: (e) => (e.kind === "notif" ? e.item.issue_id === "pinned-iss" : false),
};

describe("section-filter", () => {
  it("detects unread across entry kinds", () => {
    expect(entryIsUnread(notifEntry(1, { read: false }))).toBe(true);
    expect(entryIsUnread(notifEntry(1, { read: true }))).toBe(false);
    expect(entryIsUnread(channelEntry(1, { unread_count: 3 }))).toBe(true);
    expect(entryIsUnread(channelEntry(1, { unread_count: 0 }))).toBe(false);
  });

  it("reads project id from notif and channel", () => {
    expect(entryProjectId(notifEntry(1, { project_id: "p1" }))).toBe("p1");
    expect(entryProjectId(channelEntry(1, { project_id: "p2" }))).toBe("p2");
  });

  it("'all' matches everything; 'unread' filters; 'project' filters by id; 'pinned' uses predicate", () => {
    const e = notifEntry(1, { read: true, project_id: "p1", issue_id: "pinned-iss" });
    expect(entryMatchesSection(e, { id: "s", kind: "all" }, ctx)).toBe(true);
    expect(entryMatchesSection(e, { id: "s", kind: "unread" }, ctx)).toBe(false);
    expect(entryMatchesSection(e, { id: "s", kind: "project", projectId: "p1" }, ctx)).toBe(true);
    expect(entryMatchesSection(e, { id: "s", kind: "project", projectId: "pX" }, ctx)).toBe(false);
    expect(entryMatchesSection(e, { id: "s", kind: "pinned" }, ctx)).toBe(true);
  });

  it("TECH-3579 — 'favorites' kind uses the isFavorite predicate (and never matches without one)", () => {
    const fav = notifEntry(1, { issue_id: "fav-iss" });
    const other = notifEntry(1, { issue_id: "other-iss" });
    const favCtx: SectionFilterContext = {
      ...ctx,
      isFavorite: (e) => e.kind === "notif" && e.item.issue_id === "fav-iss",
    };
    expect(entryMatchesSection(fav, { id: "s", kind: "favorites" }, favCtx)).toBe(true);
    expect(entryMatchesSection(other, { id: "s", kind: "favorites" }, favCtx)).toBe(false);
    // No predicate supplied (e.g. favorites flag off) → nothing matches.
    expect(entryMatchesSection(fav, { id: "s", kind: "favorites" }, ctx)).toBe(false);
  });

  it("sorts newest-first by default and respects maxRows", () => {
    const entries = [notifEntry(1), notifEntry(3), notifEntry(2)];
    const out = selectSectionEntries(entries, { id: "s", kind: "all", maxRows: 2 }, ctx);
    expect(out.map((e) => e.time)).toEqual([3, 2]);
  });

  it("sorts oldest-first when configured", () => {
    const entries = [notifEntry(1), notifEntry(3), notifEntry(2)];
    const out = selectSectionEntries(entries, { id: "s", kind: "all", sort: "oldest" }, ctx);
    expect(out.map((e) => e.time)).toEqual([1, 2, 3]);
  });

  it("detects mentions on notif (type) and channel (context set)", () => {
    expect(entryIsMentioned(notifEntry(1, { type: "mentioned" }), ctx)).toBe(true);
    expect(entryIsMentioned(notifEntry(1, { type: "new_comment" }), ctx)).toBe(false);
    const mentionedCtx: SectionFilterContext = {
      ...ctx,
      action: { ...ctx.action, mentionedChannels: new Set(["c-men"]) },
    };
    expect(entryIsMentioned(channelEntry(1, { id: "c-men" }), mentionedCtx)).toBe(true);
    expect(entryIsMentioned(channelEntry(1, { id: "c-other" }), mentionedCtx)).toBe(false);
  });

  it("'filter' kind ANDs the enabled predicates; empty filter matches all (#4)", () => {
    const unreadPinned = notifEntry(1, { read: false, issue_id: "pinned-iss" });
    const readPinned = notifEntry(1, { read: true, issue_id: "pinned-iss" });
    const unreadOther = notifEntry(1, { read: false, issue_id: "other" });

    // Empty filter = everything.
    expect(entryMatchesSection(unreadOther, { id: "s", kind: "filter" }, ctx)).toBe(true);

    // Unread AND pinned: only the unread+pinned row qualifies.
    const cfg = { id: "s", kind: "filter" as const, filterUnread: true, filterPinned: true };
    expect(entryMatchesSection(unreadPinned, cfg, ctx)).toBe(true);
    expect(entryMatchesSection(readPinned, cfg, ctx)).toBe(false);
    expect(entryMatchesSection(unreadOther, cfg, ctx)).toBe(false);

    // Project narrow combines with the toggles.
    const proj = notifEntry(1, { read: false, project_id: "p1" });
    expect(
      entryMatchesSection(proj, { id: "s", kind: "filter", filterUnread: true, projectId: "p1" }, ctx),
    ).toBe(true);
    expect(
      entryMatchesSection(proj, { id: "s", kind: "filter", filterUnread: true, projectId: "pX" }, ctx),
    ).toBe(false);
  });

  it("'filter' builder ANDs explicit conditions; seeds from legacy toggles (#5)", () => {
    const unreadPinned = notifEntry(1, { read: false, issue_id: "pinned-iss" });
    const readPinned = notifEntry(1, { read: true, issue_id: "pinned-iss" });

    // Explicit builder conditions: unread AND pinned.
    const cfg = {
      id: "s",
      kind: "filter" as const,
      filters: [{ field: "unread" as const }, { field: "pinned" as const }],
    };
    expect(entryMatchesSection(unreadPinned, cfg, ctx)).toBe(true);
    expect(entryMatchesSection(readPinned, cfg, ctx)).toBe(false);

    // Empty filters array = everything (the builder starts empty).
    expect(
      entryMatchesSection(readPinned, { id: "s", kind: "filter", filters: [] }, ctx),
    ).toBe(true);

    // sectionFilters seeds from the legacy booleans when `filters` is absent.
    expect(
      sectionFilters({ id: "s", kind: "filter", filterUnread: true, projectId: "p1" }),
    ).toEqual([{ field: "unread" }, { field: "project", projectId: "p1" }]);
  });

  it("TECH-3502 #3 — new filter fields: read / muted / running / kind", () => {
    const read = notifEntry(1, { read: true });
    const unread = notifEntry(1, { read: false });
    const future = "2999-01-01T00:00:00Z";
    const muted = notifEntry(1, { muted_until: future });

    const matchesField = (e: DynInboxEntry, field: "read" | "muted", c = ctx) =>
      entryMatchesSection(e, { id: "s", kind: "filter", filters: [{ field }] }, c);

    expect(matchesField(read, "read")).toBe(true);
    expect(matchesField(unread, "read")).toBe(false);
    expect(matchesField(muted, "muted")).toBe(true);
    expect(matchesField(read, "muted")).toBe(false);

    // running: keyed off the action run-state maps.
    const runningCtx: SectionFilterContext = {
      ...ctx,
      action: { ...ctx.action, issueRunStates: new Map([["iss1", "active"]]) },
    };
    const runningEntry = notifEntry(1, { issue_id: "iss1" });
    expect(entryIsRunning(runningEntry, runningCtx)).toBe(true);
    expect(
      entryMatchesSection(runningEntry, { id: "s", kind: "filter", filters: [{ field: "running" }] }, runningCtx),
    ).toBe(true);
    expect(
      entryMatchesSection(runningEntry, { id: "s", kind: "filter", filters: [{ field: "running" }] }, ctx),
    ).toBe(false);

    // kind: "issue" maps to notif entries; channels don't match it.
    const issueFilter = { id: "s", kind: "filter" as const, filters: [{ field: "kind" as const, entryKind: "issue" as const }] };
    expect(entryMatchesSection(notifEntry(1), issueFilter, ctx)).toBe(true);
    expect(entryMatchesSection(channelEntry(1), issueFilter, ctx)).toBe(false);
  });

  it("TECH-3502 #3 — project condition matches sub-projects only with includeSubprojects", () => {
    const child = notifEntry(1, { project_id: "child" });
    const ctxWithTree: SectionFilterContext = {
      ...ctx,
      subProjectIds: new Map([["parent", new Set(["child", "grandchild"])]]),
    };
    const onlyParent = { id: "s", kind: "filter" as const, filters: [{ field: "project" as const, projectId: "parent" }] };
    const withSubs = {
      id: "s",
      kind: "filter" as const,
      filters: [{ field: "project" as const, projectId: "parent", includeSubprojects: true }],
    };
    expect(entryMatchesSection(child, onlyParent, ctxWithTree)).toBe(false);
    expect(entryMatchesSection(child, withSubs, ctxWithTree)).toBe(true);
    // Falls back to no-match when the tree is unavailable.
    expect(entryMatchesSection(child, withSubs, ctx)).toBe(false);
  });
});

describe("layout", () => {
  it("operator preset is a valid layout", () => {
    expect(isValidLayout(operatorPreset())).toBe(true);
  });

  it("rejects malformed layouts", () => {
    expect(isValidLayout(null)).toBe(false);
    expect(isValidLayout({ version: 99, tabs: [] })).toBe(false);
    expect(isValidLayout({ version: 1, tabs: [] })).toBe(false);
  });

  it("sectionLabel falls back to the catalog label", () => {
    expect(sectionLabel({ id: "s", kind: "running" })).toBe("Agents working");
    expect(sectionLabel({ id: "s", kind: "running", title: "  Mine  " })).toBe("Mine");
  });

  it("makeId returns unique ids across calls (TECH-3502 #1)", () => {
    const ids = new Set([makeId("tab"), makeId("tab"), makeId("tab"), makeId("tab")]);
    expect(ids.size).toBe(4);
  });

  it("dedupeLayoutIds heals colliding tab/section ids; leaves clean layouts untouched (TECH-3502 #1)", () => {
    const clean = operatorPreset();
    expect(dedupeLayoutIds(clean)).toBe(clean);

    // Two tabs sharing an id — the second must be renamed so switching works.
    const collided: InboxLayout = {
      version: 1,
      activeTabId: "tab_1",
      tabs: [
        { id: "tab_1", title: "A", sections: [{ id: "all_1", kind: "all" }] },
        { id: "tab_1", title: "B", sections: [{ id: "all_1", kind: "all" }] },
      ],
    };
    const fixed = dedupeLayoutIds(collided);
    const tabIds = fixed.tabs.map((t) => t.id);
    expect(new Set(tabIds).size).toBe(2);
    // Deterministic: same input → same output (no remount churn).
    expect(dedupeLayoutIds(collided)).toEqual(fixed);
    // activeTabId still points at a real tab.
    expect(tabIds).toContain(fixed.activeTabId);
  });

  it("saves a named user preset in the preferences blob shape", () => {
    const layout = operatorPreset();
    const presets = upsertUserInboxPreset([], {
      id: "preset_1",
      label: "  Morning queue  ",
      layout,
    });
    const blob = makeUserInboxPresetsBlob(presets);

    expect(blob).toEqual({
      version: 1,
      presets: [{ id: "preset_1", label: "Morning queue", layout }],
    });
  });

  it("loads only valid user presets from preferences", () => {
    const layout = operatorPreset();
    const presets = readUserInboxPresets({
      version: 1,
      presets: [
        { id: "preset_1", label: "Mine", layout },
        { id: "broken", label: "Broken", layout: { version: 99, tabs: [] } },
      ],
    });

    expect(presets).toHaveLength(1);
    expect(presets[0]?.label).toBe("Mine");
    expect(presets[0]?.layout).toEqual(layout);
  });

  it("replaces a user preset by name and deletes it by id", () => {
    const first = operatorPreset();
    const second = { ...operatorPreset(), activeTabId: "replacement" };
    const saved = upsertUserInboxPreset(
      [{ id: "old", label: "Mine", layout: first }],
      { id: "new", label: "Mine", layout: second },
    );

    expect(saved).toHaveLength(1);
    expect(saved[0]?.id).toBe("new");
    expect(saved[0]?.layout.activeTabId).toBe("replacement");
    expect(deleteUserInboxPreset(saved, "new")).toEqual([]);
  });
});

// TECH-3541 #2 — classic-inbox parity on the "All messages" box.
function chatEntry(time: number, over: Record<string, unknown> = {}): DynInboxEntry {
  const session = { id: makeId("ch"), agent_id: "ag1", has_unread: false, ...over };
  return { kind: "chat", id: session.id as string, time, session: session as never };
}

const noLabels = {} as Record<InboxActionCategory, string>;

describe("section-filter — classic parity (TECH-3541 #2)", () => {
  it("detects muted across notif, channel and chat", () => {
    const future = new Date(Date.now() + 60_000).toISOString();
    const past = new Date(Date.now() - 60_000).toISOString();
    expect(entryIsMuted(notifEntry(1, { muted_until: future }))).toBe(true);
    expect(entryIsMuted(notifEntry(1, { muted_until: past }))).toBe(false);
    expect(entryIsMuted(channelEntry(1, { muted_until: future } as never))).toBe(true);
    expect(entryIsMuted(chatEntry(1, { muted_until: future }))).toBe(true);
    expect(entryIsMuted(chatEntry(1))).toBe(false);
  });

  it("view filter mirrors the classic view switch (hides muted off-view)", () => {
    const future = new Date(Date.now() + 60_000).toISOString();
    const unread = notifEntry(1, { read: false });
    const mentioned = notifEntry(1, { type: "mentioned", read: false });
    const muted = notifEntry(1, { muted_until: future, read: false });
    const pinned = notifEntry(1, { issue_id: "pinned-iss", read: true });

    expect(matchesViewFilter(unread, "unread", ctx)).toBe(true);
    expect(matchesViewFilter(notifEntry(1, { read: true }), "unread", ctx)).toBe(false);
    expect(matchesViewFilter(mentioned, "mentioned", ctx)).toBe(true);
    expect(matchesViewFilter(pinned, "pinned", ctx)).toBe(true);
    // muted rows hidden everywhere except the "muted" view
    expect(matchesViewFilter(muted, "all", ctx)).toBe(false);
    expect(matchesViewFilter(muted, "muted", ctx)).toBe(true);
    // chat + dm channels both land in the "dms" view
    expect(matchesViewFilter(chatEntry(1), "dms", ctx)).toBe(true);
    expect(matchesViewFilter(channelEntry(1, { kind: "dm" } as never), "dms", ctx)).toBe(true);
    expect(matchesViewFilter(channelEntry(1, { kind: "channel" } as never), "channels", ctx)).toBe(true);
  });

  it("selectSectionEntries applies the box view filter", () => {
    const entries = [
      notifEntry(3, { read: false }),
      notifEntry(2, { read: true }),
      notifEntry(1, { read: false }),
    ];
    const out = selectSectionEntries(entries, { id: "s", kind: "all", viewFilter: "unread" }, ctx);
    expect(out.map((e) => e.time)).toEqual([3, 1]);
  });

  it("TECH-3541 (Jesper) — muted rows are hidden from every view, even with no view filter", () => {
    const future = new Date(Date.now() + 60_000).toISOString();
    const entries = [
      notifEntry(3, { read: false }),
      notifEntry(2, { muted_until: future, read: false }),
      notifEntry(1, { read: true }),
    ];
    // Default "All messages" box (no viewFilter) — muted row 2 drops out.
    const all = selectSectionEntries(entries, { id: "s", kind: "all" }, ctx);
    expect(all.map((e) => e.time)).toEqual([3, 1]);
    // The explicit "Muted" view is the only place muted rows resurface.
    const mutedView = selectSectionEntries(
      entries,
      { id: "s", kind: "all", viewFilter: "muted" },
      ctx,
    );
    expect(mutedView.map((e) => e.time)).toEqual([2]);
  });

  it("sorts by actor: last_me keeps my rows, sinks others", () => {
    const mine = notifEntry(1, { actor_type: "member", actor_id: "me" });
    const theirs = notifEntry(3, { actor_type: "agent", actor_id: "a1" });
    const out = sortEntries(
      [theirs, mine],
      { id: "s", kind: "all", sortMethod: "last_me" },
      "me",
    );
    // mine has a finite last_me signal, theirs is -Infinity → mine first
    expect(out[0]).toBe(mine);
  });

  it("groups by project with a 'No project' fallback bucket last", () => {
    const gctx: SectionGroupContext = {
      projectMap: new Map([["p1", { title: "Alpha" } as never]]),
    };
    const entries = [
      notifEntry(1, { project_id: "p1" }),
      notifEntry(2, { project_id: null }),
    ];
    const groups = groupSectionEntries(
      entries,
      { id: "s", kind: "all", groupBy: "project" },
      ctx,
      noLabels,
      gctx,
    );
    expect(groups.map((g) => g.label)).toEqual(["Alpha", "No project"]);
  });

  it("default preset starts with Secretary, then 'All messages' grouped by action", () => {
    const preset = operatorPreset();
    expect(preset.tabs).toHaveLength(1);
    expect(preset.tabs[0]?.sections).toHaveLength(2);
    expect(preset.tabs[0]?.sections[0]?.kind).toBe("secretary");
    expect(preset.tabs[0]?.sections[1]?.kind).toBe("all");
    expect(preset.tabs[0]?.sections[1]?.groupBy).toBe("action");
  });

  it("groups two levels deep with 'Then by'", () => {
    const gctx: SectionGroupContext = {
      projectMap: new Map([["p1", { title: "Alpha" } as never]]),
    };
    const entries = [
      notifEntry(1, { project_id: "p1", type: "new_comment" }),
      notifEntry(2, { project_id: "p1", type: "mentioned" }),
    ];
    const groups = groupSectionEntries(
      entries,
      { id: "s", kind: "all", groupBy: "project", groupBySecondary: "type" },
      ctx,
      noLabels,
      gctx,
    );
    expect(groups).toHaveLength(1);
    expect(groups[0]?.label).toBe("Alpha");
    expect(groups[0]?.entries).toEqual([]);
    expect(groups[0]?.children).toHaveLength(2);
  });
});
