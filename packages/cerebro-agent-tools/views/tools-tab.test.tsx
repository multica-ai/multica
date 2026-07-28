// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      cerebroRequest: mockCerebroRequest,
    },
  };
});

import { CerebroToolsTab } from "./tools-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Tine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderToolsTab(agent: Agent = baseAgent, canEdit = true) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroToolsTab agent={agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockCerebroRequest.mockResolvedValue([]);
});

describe("CerebroToolsTab", () => {
  it("uses the unified permission surface without a legacy feature switch", async () => {
    renderToolsTab();

    expect(await screen.findByTestId("tool-policy-table")).toBeInTheDocument();
    expect(
      screen.queryByTestId("simple-tool-policy-table"),
    ).not.toBeInTheDocument();
  });

  it("binds the agent's owner as the user ceiling on the agent page", async () => {
    renderToolsTab();

    // The Effective column must reflect the full Runtime › Agent › Group › User
    // chain, so the table fetch carries the agent's runtime, the agent itself,
    // AND the owner's user id (the groups are expanded server-side from it).
    await waitFor(() => expect(mockCerebroRequest).toHaveBeenCalled());
    const urls = mockCerebroRequest.mock.calls.map(([url]) => String(url));
    const tableUrl = urls.find((u) => u.includes("/tool-policy?"));
    expect(tableUrl).toBeDefined();
    expect(tableUrl).toContain("agent_id=agent-1");
    expect(tableUrl).toContain("runtime_id=runtime-1");
    expect(tableUrl).toContain("user_id=user-1");
  });
});
