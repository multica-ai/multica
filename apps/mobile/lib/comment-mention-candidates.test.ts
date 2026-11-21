// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, MemberWithUser, Squad } from "@multica/core/types";
import { commentMentionCandidates } from "./comment-mention-candidates";

const members = [
  { user_id: "u-2", name: "Zoe" },
  { user_id: "u-1", name: "Ada" },
] as MemberWithUser[];
const agents = [{ id: "a-1", name: "Lambda" }] as Agent[];
const squads = [
  { id: "s-1", name: "Relay", archived_at: null },
  { id: "s-2", name: "Old squad", archived_at: "2026-01-01T00:00:00Z" },
] as Squad[];

describe("commentMentionCandidates", () => {
  it("offers a member @all, people, agents and live squads, sorted", () => {
    const result = commentMentionCandidates({ query: "", members, agents, squads, guest: false });

    expect(result.all).toBe(true);
    expect(result.members.map((m) => m.name)).toEqual(["Ada", "Zoe"]);
    expect(result.agents.map((a) => a.id)).toEqual(["a-1"]);
    expect(result.squads.map((s) => s.id)).toEqual(["s-1"]);
  });

  it("offers a guest people only: no @all, agents or squads (MUL-9420)", () => {
    const result = commentMentionCandidates({ query: "", members, agents, squads, guest: true });

    expect(result).toEqual({ all: false, members: [members[1], members[0]], agents: [], squads: [] });
  });

  it("filters every section by the query", () => {
    const result = commentMentionCandidates({ query: " lam ", members, agents, squads, guest: false });

    expect(result).toEqual({ all: false, members: [], agents: [agents[0]], squads: [] });
  });
});
