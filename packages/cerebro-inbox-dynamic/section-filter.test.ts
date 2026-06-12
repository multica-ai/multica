import { describe, it, expect } from "vitest";
import type { InboxItem, Channel } from "@multica/core/types";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import {
  entryIsUnread,
  entryProjectId,
  entryMatchesSection,
  selectSectionEntries,
  type DynInboxEntry,
  type SectionFilterContext,
} from "./section-filter";
import { isValidLayout, operatorPreset, sectionLabel, makeId } from "./layout";

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
});
