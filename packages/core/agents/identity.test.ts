import { describe, expect, it } from "vitest";
import { isGitLabPersonaAgent } from "./identity";

describe("isGitLabPersonaAgent", () => {
  it("matches gitlab: system keys", () => {
    expect(isGitLabPersonaAgent({ system_key: "gitlab:4242" })).toBe(true);
    expect(isGitLabPersonaAgent({ system_key: "gitlab:u:alice" })).toBe(true);
  });

  it("rejects normal agents and other system keys", () => {
    expect(isGitLabPersonaAgent({ system_key: null })).toBe(false);
    expect(isGitLabPersonaAgent({ system_key: undefined })).toBe(false);
    expect(isGitLabPersonaAgent({ system_key: "agent_builder:abc" })).toBe(
      false,
    );
    expect(isGitLabPersonaAgent({})).toBe(false);
  });
});
