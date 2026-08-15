import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "../api";
import type { Agent } from "../types";
import {
  qianwenAgentListOptions,
  qianwenInstallationsOptions,
  qianwenKeys,
} from "./queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "workspace-1",
    runtime_id: "runtime-1",
    name: "Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: null,
    skills: [],
    created_at: "2026-08-15T00:00:00Z",
    updated_at: "2026-08-15T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("Qianwen installation queries", () => {
  it("projects legacy agent cache records to a camelCase Qianwen view model", () => {
    const activeOwnedAgent = makeAgent({
      id: "agent-owned",
      name: "Owned Agent",
      owner_id: "user-1",
      archived_at: null,
      permission_mode: "private",
      invocation_targets: [],
    });
    const archivedWorkspaceAgent = makeAgent({
      id: "agent-workspace",
      name: "Workspace Agent",
      owner_id: "user-2",
      archived_at: "2026-08-15T03:00:00Z",
      permission_mode: "public_to",
      invocation_targets: [
        { target_type: "workspace", target_id: "workspace-1" },
      ],
    });

    const select = qianwenAgentListOptions("workspace-1", {
      userId: "user-1",
      role: "member",
    }).select;

    expect(select?.([activeOwnedAgent, archivedWorkspaceAgent])).toEqual([
      {
        id: "agent-owned",
        name: "Owned Agent",
        archivedAt: null,
        canManage: true,
        canInvoke: true,
      },
      {
        id: "agent-workspace",
        name: "Workspace Agent",
        archivedAt: "2026-08-15T03:00:00Z",
        canManage: false,
        canInvoke: true,
      },
    ]);
  });

  it("isolates caller-relative binding state by workspace and current user", () => {
    expect(qianwenKeys.installations("workspace-1", "user-1")).toEqual([
      "qianwen",
      "workspace-1",
      "installations",
      "user",
      "user-1",
    ]);
    expect(qianwenKeys.installations("workspace-1", "user-1")).not.toEqual(
      qianwenKeys.installations("workspace-1", "user-2"),
    );

    expect(qianwenInstallationsOptions("workspace-1", "user-1").enabled).toBe(true);
    expect(qianwenInstallationsOptions("workspace-1", "").enabled).toBe(false);
  });

  it("never stores unknown config or access-token fields in the Query cache", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            installations: [
              {
                id: "installation-1",
                agent_id: "agent-1",
                connection_id: "qwc_connection-1",
                mode: "personal_polling",
                status: "active",
                current_user_bound: false,
                config: { access_token: "qws_nested-cache-secret" },
                access_token: "qws_row-cache-secret",
              },
            ],
            configured: true,
            pairing_supported: true,
            config: { access_token: "qws_top-level-config-cache-secret" },
            access_token: "qws_top-level-cache-secret",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    setApiInstance(new ApiClient("https://api.example.test"));
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    try {
      const options = qianwenInstallationsOptions("workspace-1", "user-1");
      const parsed = await queryClient.fetchQuery(options);
      const cached = queryClient.getQueryData(
        qianwenKeys.installations("workspace-1", "user-1"),
      );

      expect(parsed).toEqual({
        installations: [
          {
            id: "installation-1",
            agentId: "agent-1",
            connectionId: "qwc_connection-1",
            mode: "personal_polling",
            status: "active",
            currentUserBound: false,
          },
        ],
        configured: true,
        pairingSupported: true,
      });
      expect(cached).toEqual(parsed);
      const serializedCache = JSON.stringify(cached);
      expect(serializedCache).not.toContain('"config":');
      expect(serializedCache).not.toContain("access_token");
      expect(serializedCache).not.toContain("qws_");
    } finally {
      queryClient.clear();
    }
  });
});
