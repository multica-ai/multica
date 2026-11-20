// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@multica/core/types";
import { canDeleteProject } from "@multica/core/projects/deletion-permissions";

import { agentListOptions } from "./agents";

const mocks = vi.hoisted(() => ({ listAgents: vi.fn() }));
vi.mock("@/data/api", () => ({ api: { listAgents: mocks.listAgents } }));

beforeEach(() => vi.clearAllMocks());

describe("agentListOptions", () => {
  it("loads archived creators for project permissions without replacing the active-agent cache", async () => {
    const archived = { id: "creator-agent", owner_id: "me", archived_at: "2026-11-04T00:00:00Z" };
    mocks.listAgents.mockImplementation(async ({ includeArchived }) => includeArchived ? [archived] : []);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
    try {
      const activeOptions = agentListOptions("workspace-1");
      const permissionOptions = agentListOptions("workspace-1", { includeArchived: true });
      const active = await client.fetchQuery(activeOptions);
      const all = await client.fetchQuery(permissionOptions);
      const project = { creator_type: "agent" as const, creator_id: archived.id };

      expect(canDeleteProject(project, "me", "member", active)).toBe(false);
      expect(canDeleteProject(project, "me", "member", all)).toBe(true);
      expect(canDeleteProject(project, "someone-else", "member", all)).toBe(false);
      expect(permissionOptions.queryKey).not.toEqual(activeOptions.queryKey);
      expect(client.getQueryData(activeOptions.queryKey)).toEqual([]);
      expect(mocks.listAgents).toHaveBeenLastCalledWith(expect.objectContaining({ includeArchived: true, signal: expect.any(AbortSignal) }));

      await client.invalidateQueries({ queryKey: ["agents", "workspace-1"] });
      expect(client.getQueryState(permissionOptions.queryKey)?.isInvalidated).toBe(true);
      expect(client.getQueryState(activeOptions.queryKey)?.isInvalidated).toBe(true);
    } finally {
      client.clear();
    }
  });

  it("polls only while projected runtime availability can age offline", () => {
    const options = agentListOptions("ws-1");
    const interval = options.refetchInterval;
    expect(typeof interval).toBe("function");
    if (typeof interval !== "function") return;

    const queryState = (data: Agent[]) =>
      interval({ state: { status: "success", data } } as never);

    expect(queryState([{ runtime_availability: "online" } as Agent])).toBe(30_000);
    expect(queryState([{ runtime_availability: "unstable" } as Agent])).toBe(30_000);
    expect(queryState([{ runtime_availability: "offline" } as Agent])).toBe(false);
    expect(queryState([{ runtime_availability: undefined } as Agent])).toBe(false);
    expect(
      queryState([
        {
          archived_at: "2026-09-04T00:00:00Z",
          runtime_availability: "online",
        } as Agent,
      ]),
    ).toBe(false);
    expect(queryState([])).toBe(false);
  });
});
