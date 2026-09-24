// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { NeedsMeItem } from "@multica/core/home";
import type { TimelineEntry } from "@multica/core/types";
import { pickSourceComment } from "./use-source-comment";

function entry(id: string, actor_type: string, actor_id: string, extra: Partial<TimelineEntry> = {}): TimelineEntry {
  return { type: "comment", id, actor_type, actor_id, content: `body ${id}`, created_at: "2026-09-21T00:00:00Z", ...extra };
}

function item(overrides: Partial<NeedsMeItem>): NeedsMeItem {
  return {
    key: "issue:i1",
    kind: "blocked",
    issueId: "i1",
    issue: null,
    inbox: null,
    inboxIds: [],
    title: "Issue",
    identifier: "MUL-1",
    priority: "none",
    dueDate: null,
    since: "2026-09-21T00:00:00Z",
    actor: { type: "agent", id: "linus" },
    ...overrides,
  };
}

describe("pickSourceComment", () => {
  const timeline = [
    entry("c1", "agent", "linus"),
    entry("c2", "member", "me"),
    entry("c3", "agent", "linus"),
    entry("c4", "agent", "other"),
    entry("gone", "agent", "linus", { deleted_at: "2026-09-21T01:00:00Z" }),
    { ...entry("a1", "agent", "linus"), type: "activity" as const },
  ];

  it("takes the waiting agent's newest live comment", () => {
    expect(pickSourceComment(item({}), timeline, "me")?.id).toBe("c3");
  });

  it("uses the exact mentioning comment for a mention", () => {
    const mention = item({
      kind: "mentioned",
      actor: { type: "member", id: "someone" },
      inbox: { details: { comment_id: "c1" } } as unknown as NeedsMeItem["inbox"],
    });
    expect(pickSourceComment(mention, timeline, "me")?.id).toBe("c1");
  });

  it("falls back to the newest comment not written by the viewer", () => {
    const mine = item({ kind: "in_review", actor: { type: "member", id: "me" } });
    expect(pickSourceComment(mine, [entry("x", "agent", "a"), entry("y", "member", "me")], "me")?.id).toBe("x");
  });

  it("returns null when there is nothing to show", () => {
    expect(pickSourceComment(item({}), [], "me")).toBeNull();
  });
});
