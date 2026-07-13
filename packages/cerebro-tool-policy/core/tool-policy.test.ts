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
  isLockedFromElsewhere,
  permissionDescription,
  setToolPolicy,
} from "./tool-policy";
import type { ToolPolicyRow } from "./tool-policy";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

describe("permissionDescription", () => {
  const row = { description: "Create a new issue.", description_zh: "创建一个新工单。" };

  it("returns Chinese for any zh* locale when present", () => {
    expect(permissionDescription(row, "zh")).toBe("创建一个新工单。");
    expect(permissionDescription(row, "zh-Hans")).toBe("创建一个新工单。");
    expect(permissionDescription(row, "zh-Hans-CN")).toBe("创建一个新工单。");
  });

  it("returns English for non-zh locales", () => {
    expect(permissionDescription(row, "en")).toBe("Create a new issue.");
    expect(permissionDescription(row, "en-US")).toBe("Create a new issue.");
    expect(permissionDescription(row, undefined)).toBe("Create a new issue.");
  });

  it("falls back to English when the Chinese translation is missing", () => {
    expect(permissionDescription({ description: "X", description_zh: "" }, "zh")).toBe("X");
  });

  it("returns empty string when neither description is set", () => {
    expect(permissionDescription({ description: "", description_zh: "" }, "zh")).toBe("");
    expect(permissionDescription({ description: "", description_zh: "" }, "en")).toBe("");
  });
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

  it("surfaces the per-layer WHEN condition, and a malformed one drifts to null (FIR-1609)", async () => {
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "web_fetch",
          title: "Fetch a URL",
          category: "Web",
          source: "builtin",
          layers: { agent: "allow" },
          // agent carries a real host-allowlist condition; user is malformed and
          // must fail soft to null (a condition never widens access on its own).
          conditions: {
            agent: { host_allowlist: ["firtal.com"], actions: [], expr: "" },
            user: "not-an-object",
          },
          effective: { setting: "allow", decided_by: "agent", capped_by: "", reason: "" },
        },
      ],
    });

    const rows = await fetchToolPolicyTable("ws1", { agentId: "a1", userId: "u1" });
    const row = rows[0]!;
    expect(row.conditions.agent?.host_allowlist).toEqual(["firtal.com"]);
    expect(row.conditions.user).toBeNull();
    // A row that omits conditions entirely parses to all-null, never undefined.
    expect(row.conditions.workspace).toBeNull();
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

  it("PUTs the optional WHEN condition alongside the choice (FIR-1609)", async () => {
    mockCerebroRequest.mockResolvedValue(undefined);
    await setToolPolicy("ws1", {
      tool_key: "web_fetch",
      layer: "agent",
      subject_id: "a1",
      setting: "allow",
      condition: { host_allowlist: ["firtal.com", "*.firtal.com"], actions: [], arg_allowlist: [], expr: "" },
    });
    const [, init] = mockCerebroRequest.mock.calls[0]!;
    expect(JSON.parse(init.body).condition).toEqual({
      host_allowlist: ["firtal.com", "*.firtal.com"],
      actions: [],
      arg_allowlist: [],
      expr: "",
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

// --- isLockedFromElsewhere --------------------------------------------------

describe("isLockedFromElsewhere", () => {
  const NO_LAYERS: ToolPolicyRow["layers"] = {
    workspace: null,
    runtime: null,
    agent: null,
    group: null,
    user: null,
    system: null,
  };
  const NO_CONDITIONS: ToolPolicyRow["conditions"] = {
    workspace: null,
    runtime: null,
    agent: null,
    user: null,
    system: null,
  };

  function makeRow(
    effective: Partial<ToolPolicyRow["effective"]> = {},
    layers: Partial<ToolPolicyRow["layers"]> = {},
  ): ToolPolicyRow {
    return {
      tool_key: "slack.post_message",
      resource_pattern: "",
      title: "Post Slack message",
      category: "MCP · Slack",
      source: "scan",
      managed_externally: false,
      layers: { ...NO_LAYERS, ...layers },
      conditions: { ...NO_CONDITIONS },
      effective: {
        setting: "deny",
        decided_by: "workspace",
        capped_by: "",
        reason: "",
        openable: true,
        ...effective,
      },
      capped_by_groups: [],
    };
  }

  // The reported bug (FIR-3091 punkt 1): under member-override a Group Allow
  // opens a plain workspace Deny — the Group control must NOT read as locked, so
  // no false "no effect" warning fires.
  it("does NOT lock a Group editing an openable workspace Deny", () => {
    const row = makeRow(
      { setting: "deny", decided_by: "workspace", openable: true },
      { workspace: "deny" },
    );
    expect(isLockedFromElsewhere(row, "group")).toBe(false);
    expect(isLockedFromElsewhere(row, "user")).toBe(false);
    expect(isLockedFromElsewhere(row, "agent")).toBe(false);
  });

  // A workspace Disable is the one unopenable member floor — still locked.
  it("locks any member layer when the workspace layer is Disable", () => {
    const row = makeRow(
      { setting: "deny", decided_by: "workspace", openable: true },
      { workspace: "disable" },
    );
    expect(isLockedFromElsewhere(row, "group")).toBe(true);
    expect(isLockedFromElsewhere(row, "user")).toBe(true);
    expect(isLockedFromElsewhere(row, "agent")).toBe(true);
  });

  it("locks a broader member layer when a more specific member layer decided", () => {
    const row = makeRow(
      { setting: "deny", decided_by: "user", openable: true },
      { workspace: "deny", user: "deny" },
    );
    // Group is broader than the deciding User layer → cannot change the verdict.
    expect(isLockedFromElsewhere(row, "group")).toBe(true);
    expect(isLockedFromElsewhere(row, "workspace")).toBe(true);
    // The User layer itself decided → editing it is not locked.
    expect(isLockedFromElsewhere(row, "user")).toBe(false);
  });

  it("locks the Agent layer on an explicit Group/User ceiling but not on a workspace default", () => {
    const wsDefault = makeRow(
      { setting: "deny", decided_by: "workspace", openable: true },
      { workspace: "deny" },
    );
    expect(isLockedFromElsewhere(wsDefault, "agent")).toBe(false);

    const userCeiling = makeRow(
      { setting: "deny", decided_by: "user", openable: true },
      { workspace: "deny", user: "deny" },
    );
    expect(isLockedFromElsewhere(userCeiling, "agent")).toBe(true);
  });

  it("locks via a genuine tighten-cap regardless of mode", () => {
    const row = makeRow({ setting: "deny", decided_by: "agent", capped_by: "agent", openable: true });
    expect(isLockedFromElsewhere(row, "group")).toBe(true);
  });

  it("never locks when the verdict is already Allow", () => {
    const row = makeRow({ setting: "allow", decided_by: "user", openable: true });
    expect(isLockedFromElsewhere(row, "group")).toBe(false);
  });

  it("keeps the tighten-only rule for a hard floor (openable=false)", () => {
    // A hard floor (e.g. credential.*) resolves tighten-only: a workspace Deny is
    // an unopenable ceiling for a Group/Agent, exactly as before FIR-3091.
    const floor = makeRow(
      { setting: "deny", decided_by: "workspace", openable: false },
      { workspace: "deny" },
    );
    expect(isLockedFromElsewhere(floor, "group")).toBe(true);
    expect(isLockedFromElsewhere(floor, "agent")).toBe(true);
    // The deciding layer editing itself is not locked.
    expect(isLockedFromElsewhere(floor, "workspace")).toBe(false);
  });
});
