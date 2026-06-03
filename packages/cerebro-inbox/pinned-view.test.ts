import { describe, it, expect } from "vitest";
import { entryMatchesPins, type PinnedContext } from "./pinned-view";

function ctx(over: Partial<Record<keyof PinnedContext, string[]>> = {}): PinnedContext {
  return {
    pinnedIssueIds: new Set(over.pinnedIssueIds ?? []),
    pinnedProjectIds: new Set(over.pinnedProjectIds ?? []),
    pinnedChannelIds: new Set(over.pinnedChannelIds ?? []),
    childOfPinnedIds: new Set(over.childOfPinnedIds ?? []),
  };
}

describe("entryMatchesPins", () => {
  it("matches a notif whose issue is pinned", () => {
    const entry = { kind: "notif" as const, item: { issue_id: "i1", project_id: null } };
    expect(entryMatchesPins(entry, ctx({ pinnedIssueIds: ["i1"] }))).toBe(true);
  });

  it("matches a notif whose parent issue is pinned (child of pinned)", () => {
    const entry = { kind: "notif" as const, item: { issue_id: "child", project_id: null } };
    expect(entryMatchesPins(entry, ctx({ childOfPinnedIds: ["child"] }))).toBe(true);
  });

  it("matches a notif whose project is pinned", () => {
    const entry = { kind: "notif" as const, item: { issue_id: null, project_id: "p1" } };
    expect(entryMatchesPins(entry, ctx({ pinnedProjectIds: ["p1"] }))).toBe(true);
  });

  it("rejects a notif unrelated to any pin", () => {
    const entry = { kind: "notif" as const, item: { issue_id: "x", project_id: "y" } };
    expect(entryMatchesPins(entry, ctx({ pinnedIssueIds: ["i1"] }))).toBe(false);
  });

  it("matches a pinned channel and rejects an unpinned one", () => {
    expect(
      entryMatchesPins({ kind: "channel", channel: { id: "c1" } }, ctx({ pinnedChannelIds: ["c1"] })),
    ).toBe(true);
    expect(
      entryMatchesPins({ kind: "channel", channel: { id: "c2" } }, ctx({ pinnedChannelIds: ["c1"] })),
    ).toBe(false);
  });

  it("never matches an agent chat session", () => {
    expect(
      entryMatchesPins({ kind: "chat" }, ctx({ pinnedIssueIds: ["i1"], pinnedChannelIds: ["c1"] })),
    ).toBe(false);
  });

  it("returns false for everything when nothing is pinned", () => {
    expect(
      entryMatchesPins({ kind: "notif", item: { issue_id: "i1", project_id: "p1" } }, ctx()),
    ).toBe(false);
  });
});
