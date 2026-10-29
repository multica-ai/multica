// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseTeamSelection, teamHref } from "./selection";

const parse = (q: string) => parseTeamSelection(new URLSearchParams(q));

describe("parseTeamSelection", () => {
  it("defaults each kind to its first tab and rejects tabs it does not offer", () => {
    expect(parse("agent=a1")).toEqual({ kind: "agent", id: "a1", tab: "chat", sessionId: null });
    expect(parse("squad=s1&tab=chat")).toEqual({ kind: "squad", id: "s1", tab: "work" });
    expect(parse("member=u1&tab=setup")).toEqual({ kind: "member", id: "u1", tab: "work" });
  });

  it("keeps an agent's earlier conversation", () => {
    expect(parse("agent=a1&tab=chat&session=c9")).toMatchObject({ sessionId: "c9" });
  });

  it("is none without a target", () => {
    expect(parse("tab=work")).toEqual({ kind: "none" });
  });
});

describe("teamHref", () => {
  it("omits the default tab and round-trips", () => {
    expect(teamHref("/ws/team", { kind: "agent", id: "a1", tab: "chat" })).toBe("/ws/team?agent=a1");
    const href = teamHref("/ws/team", { kind: "agent", id: "a1", tab: "chat", sessionId: "c9" });
    expect(parse(href.split("?")[1]!)).toEqual({
      kind: "agent",
      id: "a1",
      tab: "chat",
      sessionId: "c9",
    });
    expect(teamHref("/ws/team", { kind: "squad", id: "s1", tab: "setup" })).toBe(
      "/ws/team?squad=s1&tab=setup",
    );
  });
});
