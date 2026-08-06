import { describe, expect, it } from "vitest";
import { buildCheckInRequestMessage } from "./check-in-request";
import type { Rock, RockCheckIn, Terminology } from "./types";

const terminology: Terminology = {
  strategy: "Strategy", rock: "Rock", rocks: "Rocks",
  vision_plan: "Vision", meetings: "Meetings", org_chart: "Org chart",
  scorecard: "Scorecard", issues_list: "Issues", strategy_map: "Strategy map",
};

const checkIn = (over: Partial<RockCheckIn> = {}): RockCheckIn => ({
  id: "c1", confidence: 50, reported_health: "at_risk", note: "Slipping on hiring",
  created_by_type: "member", created_by_id: "u1", created_at: "2026-07-20T10:00:00Z", ...over,
});

const rock = (over: Partial<Rock> = {}): Rock => ({
  id: "r1", workspace_id: "w1", title: "Ship the new checkout",
  period_id: "p1", period_name: "Q3 2026", period_start: "2026-07-01", period_end: "2026-09-30",
  confidence: 70, reported_health: "on_track",
  derived_health: { state: "on_track", reason: "", calculated_at: "2026-07-20T10:00:00Z" },
  health_score: 70, issue_count: 8, done_issue_count: 3, blocked_issue_count: 0, project_count: 1,
  projects: [], issues: [], check_ins: [],
  created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-20T10:00:00Z", ...over,
});

describe("buildCheckInRequestMessage", () => {
  it("puts everything the owner needs to answer inside the message", () => {
    const message = buildCheckInRequestMessage(rock(), terminology);
    expect(message).toContain("Check-in: Ship the new checkout");
    expect(message).toContain("Your Rock for Q3 2026");
    expect(message).toContain("70% confidence");
    expect(message).toContain("On track");
    expect(message).toContain("3 of 8 issues done");
    expect(message).toContain("Reply here");
  });

  it("quotes the newest check-in when there is one", () => {
    const message = buildCheckInRequestMessage(rock({ check_ins: [checkIn()] }), terminology);
    expect(message).toContain("Last check-in Jul 20: Slipping on hiring");
    expect(message).not.toContain("No check-in has been given yet");
  });

  it("says so when no check-in has been given yet", () => {
    expect(buildCheckInRequestMessage(rock(), terminology)).toContain("No check-in has been given yet");
  });

  it("falls back to a readable line when the newest check-in has no note", () => {
    const message = buildCheckInRequestMessage(rock({ check_ins: [checkIn({ note: "" })] }), terminology);
    expect(message).toContain("Last check-in Jul 20: no note");
  });

  it("renders an unreported health as plain words, not the raw value", () => {
    const message = buildCheckInRequestMessage(rock({ reported_health: "unknown" }), terminology);
    expect(message).toContain("not reported yet");
    expect(message).not.toContain("unknown");
  });

  it("links back to the rocks page when a path is given, and omits the link otherwise", () => {
    expect(buildCheckInRequestMessage(rock(), terminology, "/firtal/rocks")).toContain("[Open Rocks](/firtal/rocks)");
    expect(buildCheckInRequestMessage(rock(), terminology, null)).not.toContain("[Open Rocks]");
  });

  it("uses the workspace's own wording for a rock", () => {
    const message = buildCheckInRequestMessage(rock(), { ...terminology, rock: "Goal", rocks: "Goals" }, "/firtal/rocks");
    expect(message).toContain("Your Goal for Q3 2026");
    expect(message).toContain("[Open Goals](/firtal/rocks)");
  });
});
