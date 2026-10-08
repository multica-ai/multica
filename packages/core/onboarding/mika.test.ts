import { describe, expect, it } from "vitest";
import { isMikaAgent, memberNeedsMikaSetup, workspaceNeedsMika } from "./mika";

const mika = { id: "agent-mika", system_key: "mika" };
const other = { id: "agent-other", system_key: undefined };
const kicked = {
  agent_id: "agent-mika",
  last_message: { content: "…", role: "user" as const, created_at: "2026-01-01" },
};

describe("workspaceNeedsMika", () => {
  it("is true for an empty workspace", () => {
    expect(workspaceNeedsMika([])).toBe(true);
  });

  // The regression this guards: the Runtimes recovery card gated on
  // `agents.length === 0`, so creating any ordinary agent first hid the only
  // surface that can mint a Mika. The generic agent endpoint accepts neither
  // `kind` nor `system_key`, so there was no other way back.
  it("stays true when the workspace has ordinary agents but no Mika", () => {
    expect(
      workspaceNeedsMika([{ system_key: undefined }, { system_key: "" }]),
    ).toBe(true);
  });

  it("is false once a Mika exists", () => {
    expect(workspaceNeedsMika([{ system_key: "mika" }])).toBe(false);
    expect(
      workspaceNeedsMika([{ system_key: undefined }, { system_key: "mika" }]),
    ).toBe(false);
  });

  // Identity is the system key, never the display name — Mika is renameable.
  it("does not treat a renamed-to-Mika ordinary agent as Mika", () => {
    expect(isMikaAgent({ system_key: undefined })).toBe(false);
    expect(isMikaAgent({ system_key: "agent_builder" })).toBe(false);
  });
});

// Bootstrapping is three server steps and the last two can fail after the
// agent commits. Gating the entrypoint on the agent alone meant the agent's
// own `agent:created` broadcast tore the card down the moment step one
// succeeded — and it never returned, because the agent is durable and the rest
// was not. These cases are exactly the partial states that produced.
describe("memberNeedsMikaSetup", () => {
  it("is true when the workspace has no Mika at all", () => {
    expect(memberNeedsMikaSetup([other], [])).toBe(true);
  });

  it("stays true when the agent exists but the member has no session", () => {
    expect(memberNeedsMikaSetup([mika], [])).toBe(true);
  });

  it("stays true when the session exists but the kickoff never landed", () => {
    expect(
      memberNeedsMikaSetup([mika], [{ agent_id: "agent-mika", last_message: null }]),
    ).toBe(true);
  });

  // Sessions are per member, so another agent's conversation is not this one.
  it("ignores sessions belonging to other agents", () => {
    expect(
      memberNeedsMikaSetup([mika], [{ ...kicked, agent_id: "agent-other" }]),
    ).toBe(true);
  });

  it("is false once the member has a kicked-off Mika conversation", () => {
    expect(memberNeedsMikaSetup([other, mika], [kicked])).toBe(false);
  });
});
