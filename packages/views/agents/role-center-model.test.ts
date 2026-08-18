// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  AGENT_ROLE_CENTER_TABS,
  agentRoleCenterCreatePath,
  agentRoleCenterWorkPath,
} from "./role-center-model";

describe("Agents role center model", () => {
  it("keeps only the approved role-center surfaces", () => {
    expect(AGENT_ROLE_CENTER_TABS).toEqual([
      "overview",
      "skills",
      "instructions",
      "general",
    ]);
  });

  it("sends creation to the manual role form and work to Tasks", () => {
    expect(agentRoleCenterCreatePath("/studio-a")).toBe(
      "/studio-a/agents/new/manual",
    );
    expect(agentRoleCenterWorkPath("/studio-a")).toBe("/studio-a/issues");
  });
});
