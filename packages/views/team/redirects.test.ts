// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatSession } from "@multica/core/types";
import { teamHrefForAgent, teamHrefForChat, teamHrefForMember, teamHrefForSquad } from "./redirects";

const session = (id: string, extra: Partial<ChatSession> = {}): ChatSession =>
  ({
    id,
    workspace_id: "ws",
    agent_id: "a1",
    creator_id: "u1",
    title: "",
    status: "active",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...extra,
  }) as ChatSession;

const q = (s: string) => new URLSearchParams(s);

describe("teamHrefForChat", () => {
  const sessions = [
    session("old", { updated_at: "2026-09-01T00:00:00Z", created_at: "2026-09-01T00:00:00Z" }),
    session("current", { updated_at: "2026-10-20T00:00:00Z", created_at: "2026-10-20T00:00:00Z" }),
    session("th", { parent_chat_session_id: "current", root_chat_message_id: "m1" }),
  ];

  it("opens Team for a bare /chat and the agent for ?agent=", () => {
    expect(teamHrefForChat("/ws/team", q(""), undefined)).toBe("/ws/team");
    expect(teamHrefForChat("/ws/team", q("agent=a9"), undefined)).toBe("/ws/team?agent=a9");
  });

  it("waits for the session list before mapping a session", () => {
    expect(teamHrefForChat("/ws/team", q("session=current"), undefined)).toBeUndefined();
  });

  it("maps the current conversation, an earlier one and a thread", () => {
    expect(teamHrefForChat("/ws/team", q("session=current"), sessions)).toBe("/ws/team?agent=a1");
    expect(teamHrefForChat("/ws/team", q("session=old"), sessions)).toBe("/ws/team?agent=a1&session=old");
    expect(teamHrefForChat("/ws/team", q("session=th"), sessions)).toBe("/ws/team?agent=a1&thread=th");
    expect(teamHrefForChat("/ws/team", q("session=gone"), sessions)).toBe("/ws/team");
  });
});

describe("object pages", () => {
  it("maps agent sections onto Team tabs", () => {
    expect(teamHrefForAgent("/ws/team", "a1", q(""))).toBe("/ws/team?agent=a1");
    expect(teamHrefForAgent("/ws/team", "a1", q("view=work"))).toBe("/ws/team?agent=a1&tab=work");
    expect(teamHrefForAgent("/ws/team", "a1", q("view=skills"))).toBe("/ws/team?agent=a1&tab=setup&view=skills");
  });

  it("opens squads and members in Team", () => {
    expect(teamHrefForSquad("/ws/team", "s1")).toBe("/ws/team?squad=s1");
    expect(teamHrefForMember("/ws/team", "u1")).toBe("/ws/team?member=u1&tab=work");
  });
});
