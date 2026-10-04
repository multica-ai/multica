// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { resolveRuntimeLocalSkills, runtimeLocalSkillsKeys, runtimeLocalSkillsOptions } from "./local-skills";

vi.mock("../api", () => ({ api: { initiateListLocalSkills: vi.fn(), getListLocalSkillsResult: vi.fn() } }));
const scope = { workspaceId: "workspace", agentId: "alpha", agentUpdatedAt: "revision-1", customArgs: ["-p", "alpha"] };
const result = { id: "request", runtime_id: "runtime", status: "completed", supported: true, skills: [], created_at: "", updated_at: "" };
beforeEach(() => vi.resetAllMocks());

describe("Hermes agent catalogs", () => {
  it("separates agents, workspaces, configuration revisions and runtime imports", () => {
    const keys = [
      runtimeLocalSkillsKeys.forRuntime("runtime"),
      runtimeLocalSkillsKeys.forAgent("runtime", scope),
      runtimeLocalSkillsKeys.forAgent("runtime", { ...scope, agentId: "beta" }),
      runtimeLocalSkillsKeys.forAgent("runtime", { ...scope, workspaceId: "other" }),
      runtimeLocalSkillsKeys.forAgent("runtime", { ...scope, agentUpdatedAt: "revision-2" }),
      runtimeLocalSkillsKeys.forAgent("runtime", { ...scope, customArgs: ["-p", "beta"] }),
    ];
    expect(new Set(keys.map((key) => JSON.stringify(key))).size).toBe(keys.length);
    expect(runtimeLocalSkillsOptions("runtime", scope).queryKey).toEqual(keys[1]);
  });

  it("sends the agent selector and accepts only a matching scope", async () => {
    vi.mocked(api.initiateListLocalSkills).mockResolvedValue({ ...result, status: "completed", agent_id: "alpha" });
    await expect(resolveRuntimeLocalSkills("runtime", scope)).resolves.toMatchObject({ skills: [], supported: true });
    expect(api.initiateListLocalSkills).toHaveBeenCalledWith("runtime", "alpha");
    vi.mocked(api.initiateListLocalSkills).mockResolvedValue({ ...result, status: "completed", agent_id: "beta" });
    await expect(resolveRuntimeLocalSkills("runtime", scope)).rejects.toThrow("did not honor");
  });

  it("rejects old unscoped server responses for agents while preserving runtime imports", async () => {
    vi.mocked(api.initiateListLocalSkills).mockResolvedValue({ ...result, status: "completed" });
    await expect(resolveRuntimeLocalSkills("runtime", scope)).rejects.toThrow("did not honor");
    await expect(resolveRuntimeLocalSkills("runtime")).resolves.toMatchObject({ supported: true });
    expect(api.initiateListLocalSkills).toHaveBeenLastCalledWith("runtime", undefined);
  });
});
