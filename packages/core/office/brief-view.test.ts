import { describe, expect, it } from "vitest";
import type { Autopilot } from "../types";
import {
  findSecretaryAutopilot,
  hasUnreadBrief,
  isBriefToday,
  splitBriefSections,
} from "./brief-view";

describe("splitBriefSections", () => {
  it("returns each H2 section in order with its trimmed body", () => {
    const body = [
      "## Headline",
      "Two things need you.",
      "",
      "## Needs your decision",
      "- Approve the pricing test",
      "### Detail",
      "Kept inside the section.",
      "## FYI",
      "Cost is down.",
    ].join("\n");

    expect(splitBriefSections(body)).toEqual([
      { heading: "Headline", body: "Two things need you." },
      {
        heading: "Needs your decision",
        body: "- Approve the pricing test\n### Detail\nKept inside the section.",
      },
      { heading: "FYI", body: "Cost is down." },
    ]);
  });

  it("drops text before the first heading instead of making it a section", () => {
    expect(splitBriefSections("preamble\n## Progress\nDone.")).toEqual([
      { heading: "Progress", body: "Done." },
    ]);
  });

  it("returns no sections for a body without H2 headings", () => {
    expect(splitBriefSections("Nothing needs you today.")).toEqual([]);
  });
});

describe("isBriefToday", () => {
  const now = new Date(2026, 10, 2, 9, 0);

  it("is true for a brief created earlier the same local day", () => {
    expect(isBriefToday({ created_at: new Date(2026, 10, 2, 0, 5).toISOString() }, now)).toBe(true);
  });

  it("is false for yesterday and for an unreadable timestamp", () => {
    expect(isBriefToday({ created_at: new Date(2026, 10, 1, 23, 59).toISOString() }, now)).toBe(false);
    expect(isBriefToday({ created_at: "not a date" }, now)).toBe(false);
  });
});

describe("hasUnreadBrief", () => {
  it("is unread when a brief exists and was not the last one opened", () => {
    expect(hasUnreadBrief("b2", "b1")).toBe(true);
    expect(hasUnreadBrief("b1", null)).toBe(true);
  });

  it("is read once the newest brief has been opened, or when there is none", () => {
    expect(hasUnreadBrief("b1", "b1")).toBe(false);
    expect(hasUnreadBrief(undefined, null)).toBe(false);
  });
});

describe("findSecretaryAutopilot", () => {
  function autopilot(id: string, patch: Partial<Autopilot>): Autopilot {
    return {
      id,
      workspace_id: "ws-1",
      title: id,
      description: null,
      assignee_type: "agent",
      assignee_id: "sec-1",
      status: "active",
      execution_mode: "run_only",
      issue_title_template: null,
      created_by_type: "member",
      created_by_id: "m1",
      last_run_at: null,
      created_at: "",
      updated_at: "",
      ...patch,
    };
  }

  it("prefers the active autopilot assigned to the Secretary", () => {
    const list = [
      autopilot("weekly", { status: "paused" }),
      autopilot("squad", { assignee_type: "squad", assignee_id: "sec-1" }),
      autopilot("daily", {}),
    ];
    expect(findSecretaryAutopilot(list, "sec-1")?.id).toBe("daily");
  });

  it("falls back to a paused one and ignores archived ones", () => {
    const list = [autopilot("gone", { status: "archived" }), autopilot("weekly", { status: "paused" })];
    expect(findSecretaryAutopilot(list, "sec-1")?.id).toBe("weekly");
  });

  it("is null without a Secretary or without a matching autopilot", () => {
    expect(findSecretaryAutopilot([autopilot("daily", {})], null)).toBeNull();
    expect(findSecretaryAutopilot([autopilot("daily", {})], "other")).toBeNull();
  });
});
