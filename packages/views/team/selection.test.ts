// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseTeamSelection, teamHref } from "./selection";

const parse = (q: string) => parseTeamSelection(new URLSearchParams(q));

describe("parseTeamSelection", () => {
  it("defaults each kind to its first tab and rejects tabs it does not offer", () => {
    expect(parse("agent=a1")).toEqual({
      kind: "agent",
      id: "a1",
      tab: "chat",
      sessionId: null,
      threadId: null,
    });
    expect(parse("squad=s1&tab=chat")).toEqual({ kind: "squad", id: "s1", tab: "work" });
    expect(parse("member=u1&tab=setup")).toEqual({ kind: "member", id: "u1", tab: "chat", threadId: null });
    expect(parse("member=u1&tab=work")).toMatchObject({ tab: "work" });
  });

  it("keeps an agent's earlier conversation", () => {
    expect(parse("agent=a1&tab=chat&session=c9")).toMatchObject({ sessionId: "c9" });
  });

  it("keeps the open thread", () => {
    expect(parse("agent=a1&thread=t1")).toMatchObject({ threadId: "t1", sessionId: null });
    expect(teamHref("/ws/team", { kind: "agent", id: "a1", threadId: "t1" })).toBe(
      "/ws/team?agent=a1&thread=t1",
    );
  });

  it("opens a channel and the thread under one of its messages", () => {
    expect(parse("channel=c1&thread=t1")).toEqual({ kind: "channel", id: "c1", tab: "chat", threadId: "t1" });
    expect(teamHref("/ws/team", { kind: "channel", id: "c1", threadId: "t1" })).toBe(
      "/ws/team?channel=c1&thread=t1",
    );
    expect(teamHref("/ws/team", { kind: "member", id: "u1", tab: "work" })).toBe("/ws/team?member=u1&tab=work");
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
      threadId: null,
    });
    expect(teamHref("/ws/team", { kind: "squad", id: "s1", tab: "setup" })).toBe(
      "/ws/team?squad=s1&tab=setup",
    );
  });
});
