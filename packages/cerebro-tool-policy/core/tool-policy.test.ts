import { beforeEach, describe, expect, it, vi } from "vitest";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import {
  clearToolPolicy,
  fetchToolPolicyTable,
  setToolPolicy,
} from "./tool-policy";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

describe("fetchToolPolicyTable", () => {
  it("returns rows with per-layer settings and the resolved effective verdict", async () => {
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "slack.post_message",
          title: "Post Slack message",
          category: "Slack",
          source: "scan",
          layers: { runtime: null, agent: "allow", group: null, user: "deny" },
          effective: {
            setting: "deny",
            decided_by: "user",
            capped_by: "user",
            reason: "Capped by user",
          },
        },
      ],
    });

    const rows = await fetchToolPolicyTable("ws1", { agentId: "a1", userId: "u1" });
    expect(rows).toHaveLength(1);
    const row = rows[0]!;
    expect(row.tool_key).toBe("slack.post_message");
    expect(row.layers.agent).toBe("allow");
    expect(row.layers.user).toBe("deny");
    expect(row.effective.setting).toBe("deny");
    expect(row.effective.capped_by).toBe("user");
  });

  it("builds the query string from the context (groups repeat, base passed)", async () => {
    mockCerebroRequest.mockResolvedValue({ tools: [] });
    await fetchToolPolicyTable("ws1", {
      runtimeId: "r1",
      agentId: "a1",
      userId: "u1",
      groupIds: ["g1", "g2"],
      base: "deny",
    });
    const url = mockCerebroRequest.mock.calls[0]![0] as string;
    expect(url).toContain("/api/workspaces/ws1/tool-policy?");
    expect(url).toContain("runtime_id=r1");
    expect(url).toContain("agent_id=a1");
    expect(url).toContain("group_id=g1");
    expect(url).toContain("group_id=g2");
    expect(url).toContain("base=deny");
  });

  it("fails CLOSED: an unknown effective setting downgrades to deny, not allow", async () => {
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "mystery.tool",
          // server drifted to a setting this client doesn't know
          effective: { setting: "super_allow", decided_by: "", capped_by: "", reason: "" },
          layers: { runtime: "future_setting", agent: null, group: null, user: null },
        },
      ],
    });
    const rows = await fetchToolPolicyTable("ws1", {});
    expect(rows).toHaveLength(1);
    const row = rows[0]!;
    // Unknown effective → deny (never silently allow a permission surface).
    expect(row.effective.setting).toBe("deny");
    // Unknown per-layer setting → treated as no override.
    expect(row.layers.runtime).toBeNull();
  });

  it("returns an empty list when the response shape is malformed", async () => {
    mockCerebroRequest.mockResolvedValue({ unexpected: true });
    const rows = await fetchToolPolicyTable("ws1", {});
    expect(rows).toEqual([]);
  });

  it("returns an empty list when the response is null", async () => {
    mockCerebroRequest.mockResolvedValue(null);
    const rows = await fetchToolPolicyTable("ws1", {});
    expect(rows).toEqual([]);
  });
});

describe("setToolPolicy / clearToolPolicy", () => {
  it("PUTs the authoring choice to the tool-policy endpoint", async () => {
    mockCerebroRequest.mockResolvedValue(undefined);
    await setToolPolicy("ws1", {
      tool_key: "deploy_restart",
      layer: "agent",
      subject_id: "a1",
      setting: "ask",
    });
    const [path, init] = mockCerebroRequest.mock.calls[0]!;
    expect(path).toBe("/api/workspaces/ws1/tool-policy");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(init.body)).toEqual({
      tool_key: "deploy_restart",
      layer: "agent",
      subject_id: "a1",
      setting: "ask",
    });
  });

  it("DELETEs with the tool/layer/subject in the query string", async () => {
    mockCerebroRequest.mockResolvedValue(undefined);
    await clearToolPolicy("ws1", {
      tool_key: "deploy_restart",
      layer: "user",
      subject_id: "u1",
    });
    const [path, init] = mockCerebroRequest.mock.calls[0]!;
    expect(path).toContain("/api/workspaces/ws1/tool-policy?");
    expect(path).toContain("tool_key=deploy_restart");
    expect(path).toContain("layer=user");
    expect(path).toContain("subject_id=u1");
    expect(init.method).toBe("DELETE");
  });
});
