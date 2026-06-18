import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { AgentRunDashboard } from "../types";
import { agentRunDashboardOptions } from "./queries";

const emptyDashboard: AgentRunDashboard = {
  summary: {
    total_runs: 0,
    successful_runs: 0,
    failed_runs: 0,
    success_rate: 0,
    average_duration_seconds: 0,
    active_agent_count: 0,
  },
  daily: [],
  heatmap: [],
  failure_reasons: [],
  retry_distribution: [],
  agents: [],
  recent_failures: [],
  recent_runs: [],
};

function installFakeApi(
  getAgentRunDashboard: (params: {
    days?: number;
    agent_ids?: string[];
    owner_id?: string;
    start_hour?: number;
    end_hour?: number;
    tz?: string;
    limit?: number;
  }) => Promise<AgentRunDashboard>,
) {
  setApiInstance({ getAgentRunDashboard } as unknown as ApiClient);
}

describe("agentRunDashboardOptions", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("passes the current owner id when the dashboard defaults to the current user", async () => {
    const getAgentRunDashboard = vi.fn().mockResolvedValue(emptyDashboard);
    installFakeApi(getAgentRunDashboard);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    await qc.fetchQuery(
      agentRunDashboardOptions("ws-1", {
        days: 30,
        agentIds: [],
        ownerId: "user-1",
        startHour: 0,
        endHour: 23,
        timezone: "Asia/Shanghai",
        limit: 50,
      }),
    );

    expect(getAgentRunDashboard).toHaveBeenCalledWith(
      expect.objectContaining({ owner_id: "user-1" }),
    );
    qc.clear();
  });

  it("omits owner_id when the user explicitly selects all members", async () => {
    const getAgentRunDashboard = vi.fn().mockResolvedValue(emptyDashboard);
    installFakeApi(getAgentRunDashboard);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    await qc.fetchQuery(
      agentRunDashboardOptions("ws-1", {
        days: 30,
        agentIds: [],
        ownerId: null,
        startHour: 0,
        endHour: 23,
        timezone: "Asia/Shanghai",
        limit: 50,
      }),
    );

    expect(getAgentRunDashboard).toHaveBeenCalledWith(
      expect.not.objectContaining({ owner_id: expect.any(String) }),
    );
    qc.clear();
  });
});
