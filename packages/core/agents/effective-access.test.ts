import { describe, expect, it } from "vitest";
import type { AgentInvocationTarget } from "../types";
import {
  ALL_ACCESS_SCOPES,
  effectiveAccessScope,
  isAccessChangeReady,
} from "./effective-access";

function ws(): AgentInvocationTarget {
  return { target_type: "workspace", target_id: null };
}
function member(id: string): AgentInvocationTarget {
  return { target_type: "member", target_id: id };
}
function team(id: string): AgentInvocationTarget {
  return { target_type: "team", target_id: id };
}

describe("effectiveAccessScope", () => {
  it("maps private to owner-only", () => {
    expect(effectiveAccessScope("private", [])).toBe("owner-only");
  });

  it("maps public_to + workspace target to workspace", () => {
    expect(effectiveAccessScope("public_to", [ws()])).toBe("workspace");
  });

  it("maps public_to + member target only to specific-people", () => {
    expect(effectiveAccessScope("public_to", [member("u-1")])).toBe("specific-people");
  });

  it("maps public_to + team target only to specific-people", () => {
    expect(effectiveAccessScope("public_to", [team("t-1")])).toBe("specific-people");
  });

  it("maps public_to with no targets to specific-people", () => {
    expect(effectiveAccessScope("public_to", [])).toBe("specific-people");
  });

  it("fails safe to owner-only when permission_mode is absent", () => {
    expect(effectiveAccessScope(undefined, [ws()])).toBe("owner-only");
    expect(effectiveAccessScope(null, [ws()])).toBe("owner-only");
  });

  it("treats public_to with missing invocation_targets as specific-people", () => {
    expect(effectiveAccessScope("public_to", undefined)).toBe("specific-people");
    expect(effectiveAccessScope("public_to", null)).toBe("specific-people");
  });

  it("prefers workspace when a workspace target sits alongside member targets", () => {
    expect(effectiveAccessScope("public_to", [member("u-1"), ws()])).toBe("workspace");
  });
});

describe("ALL_ACCESS_SCOPES", () => {
  it("lists the three scopes in display order", () => {
    expect(ALL_ACCESS_SCOPES).toEqual([
      "workspace",
      "specific-people",
      "owner-only",
    ]);
  });
});

describe("isAccessChangeReady", () => {
  it("null is not ready (no scope picked yet)", () => {
    expect(isAccessChangeReady(null)).toBe(false);
  });

  it("private with no targets is ready (clears targets)", () => {
    expect(
      isAccessChangeReady({ permission_mode: "private", invocation_targets: [] }),
    ).toBe(true);
  });

  it("public_to with workspace target only is ready", () => {
    expect(
      isAccessChangeReady({
        permission_mode: "public_to",
        invocation_targets: [ws()],
      }),
    ).toBe(true);
  });

  it("public_to with member target is ready", () => {
    expect(
      isAccessChangeReady({
        permission_mode: "public_to",
        invocation_targets: [member("u-1")],
      }),
    ).toBe(true);
  });

  it("public_to with zero targets is NOT ready", () => {
    expect(
      isAccessChangeReady({ permission_mode: "public_to", invocation_targets: [] }),
    ).toBe(false);
  });
});
