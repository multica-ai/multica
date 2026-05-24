import { describe, expect, it } from "vitest";
import { csvToList, mergeCapabilitiesSettings, readCapabilitiesConfig } from "./settings";

describe("agent capability settings", () => {
  it("defaults to standard local fallback", () => {
    const cfg = readCapabilitiesConfig({});
    expect(cfg.default_profile_id).toBe("standard");
    expect(cfg.sandbox_profiles.map((p) => p.id)).toContain("strict");
  });

  it("preserves unrelated workspace settings", () => {
    const merged = mergeCapabilitiesSettings({ tasks_paused: true }, readCapabilitiesConfig({}));
    expect(merged.tasks_paused).toBe(true);
    expect(merged.agent_capabilities).toBeTruthy();
  });

  it("strips settings that are not enforced in the first slice", () => {
    const cfg = readCapabilitiesConfig({
      agent_capabilities: {
        default_profile_id: "strict",
        compliance_lock: true,
        sandbox_profiles: [
          {
            id: "strict",
            name: "Strict",
            network_allowlist: ["api.anthropic.com:443"],
            file_roots: ["/tmp"],
          },
        ],
        agent_permissions: {
          "agent-1": {
            mcp_denied_servers: ["github"],
            cost_cap_cents_per_task: 500,
          },
        },
      },
    });

    expect(cfg).toEqual({
      default_profile_id: "strict",
      sandbox_profiles: [{ id: "strict", name: "Strict", network_allowlist: ["api.anthropic.com:443"] }],
      runtime_profile_overrides: {},
      agent_permissions: {
        "agent-1": { mcp_denied_servers: ["github"] },
      },
    });
  });

  it("normalizes comma separated lists", () => {
    expect(csvToList("github, browser, github, ")).toEqual(["github", "browser"]);
  });
});
