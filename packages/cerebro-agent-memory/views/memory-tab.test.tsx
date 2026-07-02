// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

import { ApiError } from "@multica/core/api";
import { CerebroAgentMemoryTab } from "./memory-tab";

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
  persona_sandbox: "",
};

function renderTab(canEdit = true) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroAgentMemoryTab agent={baseAgent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("CerebroAgentMemoryTab", () => {
  it("renders both switches off from a well-formed response", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      can_read_memory: false,
      can_write_memory: false,
    });
    renderTab();

    const readSwitch = await screen.findByRole("switch", { name: "Read memory" });
    const writeSwitch = screen.getByRole("switch", { name: "Write memory" });
    expect(readSwitch).toHaveAttribute("aria-checked", "false");
    expect(writeSwitch).toHaveAttribute("aria-checked", "false");
  });

  it("renders an explanatory empty state on 404 (flag off or no create_memory)", async () => {
    mockCerebroRequest.mockRejectedValue(
      new ApiError("not found", 404, "Not Found"),
    );
    renderTab();

    expect(
      await screen.findByText(/Memory is not available for this agent/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });

  it("toggling a switch calls PUT with the merged next state", async () => {
    mockCerebroRequest.mockResolvedValueOnce({
      agent_id: "agent-1",
      can_read_memory: false,
      can_write_memory: false,
    });
    mockCerebroRequest.mockResolvedValueOnce({
      agent_id: "agent-1",
      can_read_memory: true,
      can_write_memory: false,
    });
    renderTab();

    const readSwitch = await screen.findByRole("switch", { name: "Read memory" });
    await userEvent.click(readSwitch);

    await waitFor(() => {
      expect(mockCerebroRequest).toHaveBeenCalledWith(
        "/api/agents/agent-1/memory-settings",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ can_read_memory: true, can_write_memory: false }),
        }),
      );
    });
  });

  it("disables both switches when canEdit is false", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      can_read_memory: false,
      can_write_memory: false,
    });
    renderTab(false);

    const readSwitch = await screen.findByRole("switch", { name: "Read memory" });
    const writeSwitch = screen.getByRole("switch", { name: "Write memory" });
    expect(readSwitch).toHaveAttribute("aria-disabled", "true");
    expect(writeSwitch).toHaveAttribute("aria-disabled", "true");
  });
});
