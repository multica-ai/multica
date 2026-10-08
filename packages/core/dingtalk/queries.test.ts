// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  dingtalkAgentGroupsOptions,
  dingtalkAgentInactiveGroupsOptions,
  dingtalkGroupsOptions,
  dingtalkInactiveGroupsOptions,
  dingtalkKeys,
} from "./queries";

describe("dingtalkGroupsOptions", () => {
  it("is workspace-scoped and polls only while discovery is healthy", () => {
    const options = dingtalkGroupsOptions("ws-1");
    expect(options.queryKey).toEqual(dingtalkKeys.groups("ws-1"));
    expect(options.enabled).toBe(true);
    const interval = options.refetchInterval;
    expect(typeof interval).toBe("function");
    if (typeof interval !== "function") return;
    expect(
      interval({
        state: {
          status: "success",
          data: { groups: [], group_discovery_supported: true },
        },
      } as never),
    ).toBe(5_000);
    expect(
      interval({
        state: {
          status: "success",
          data: { groups: [], group_discovery_supported: false },
        },
      } as never),
    ).toBe(false);
    expect(
      interval({
        state: {
          status: "success",
          data: { groups: [] },
        },
      } as never),
    ).toBe(false);
    expect(
      interval({
        state: {
          status: "success",
          data: { groups: [], group_discovery_supported: null },
        },
      } as never),
    ).toBe(false);
    expect(interval({ state: { status: "error" } } as never)).toBe(false);
  });

  it("stays disabled before workspace context is available", () => {
    expect(dingtalkGroupsOptions("").enabled).toBe(false);
  });

  it("separates each Agent detail cache from the admin workspace inventory", () => {
    const options = dingtalkAgentGroupsOptions("ws-1", "agent-1");
    expect(options.queryKey).toEqual(dingtalkKeys.agentGroups("ws-1", "agent-1"));
    expect(options.queryKey).not.toEqual(dingtalkKeys.groups("ws-1"));
    expect(options.enabled).toBe(true);
    expect(dingtalkAgentGroupsOptions("ws-1", "").enabled).toBe(false);
  });

  it("keeps inactive pages scoped to one installation", () => {
    const workspace = dingtalkInactiveGroupsOptions("ws-1", "inst-1");
    expect(workspace.queryKey).toEqual(
      dingtalkKeys.inactiveGroups("ws-1", "inst-1"),
    );
    expect(workspace.initialPageParam).toBe(0);
    expect(workspace.getNextPageParam?.({ next_offset: 20 } as never, [], 0, [])).toBe(20);

    const agent = dingtalkAgentInactiveGroupsOptions("ws-1", "agent-1", "inst-1");
    expect(agent.queryKey).toEqual(
      dingtalkKeys.agentInactiveGroups("ws-1", "agent-1", "inst-1"),
    );
    expect(dingtalkInactiveGroupsOptions("ws-1", "").enabled).toBe(false);
  });
});
