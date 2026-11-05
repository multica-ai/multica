import { describe, expect, it } from "vitest";
import type { SquadMember } from "@multica/core/types";
import { sharedSquadRuntimeId } from "./squad-runtime";

function agent(id: string): SquadMember {
  return {
    id: `sm-${id}`,
    squad_id: "squad-1",
    member_type: "agent",
    member_id: id,
    role: "",
    created_at: "",
  };
}

const human: SquadMember = {
  id: "sm-user",
  squad_id: "squad-1",
  member_type: "member",
  member_id: "user-1",
  role: "",
  created_at: "",
};

describe("sharedSquadRuntimeId", () => {
  it("returns the runtime when every live agent shares it", () => {
    const runtimes = new Map([["a", "rt-1"], ["b", "rt-1"]]);
    expect(sharedSquadRuntimeId([agent("a"), agent("b")], runtimes, new Set())).toBe("rt-1");
  });

  it("returns null when live agents run on different runtimes", () => {
    const runtimes = new Map([["a", "rt-1"], ["b", "rt-2"]]);
    expect(sharedSquadRuntimeId([agent("a"), agent("b")], runtimes, new Set())).toBeNull();
  });

  it("ignores archived agents when deciding whether runtimes are mixed", () => {
    const runtimes = new Map([["a", "rt-1"], ["b", "rt-2"]]);
    expect(sharedSquadRuntimeId([agent("a"), agent("b")], runtimes, new Set(["b"]))).toBe("rt-1");
  });

  it("returns an empty string when no live agent has a runtime", () => {
    expect(sharedSquadRuntimeId([agent("a")], new Map(), new Set())).toBe("");
    expect(sharedSquadRuntimeId([human], new Map(), new Set())).toBe("");
  });
});
