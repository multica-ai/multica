/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { companySetupKeys, type CompanySetupReadiness } from "@multica/core/company";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent, Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CompanySetupCard } from "./company-setup-card";

const admin = vi.hoisted(() => ({ value: true }));
const api = vi.hoisted(() => ({
  getCompanySetup: vi.fn(),
  setupCompany: vi.fn(),
  pinChatAgent: vi.fn(),
  listChatPinnedAgents: vi.fn(),
  listAgents: vi.fn(),
  listSquads: vi.fn(),
  listAutopilots: vi.fn(),
  listCompanyFloors: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("../lib/use-company-admin", () => ({ useCompanyAdmin: () => admin.value }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api,
}));

const NOT_SET_UP: CompanySetupReadiness = {
  runtime_online: true,
  secretary_id: null,
  executive_floor: false,
  autopilot_daily: false,
  autopilot_weekly_paused: false,
  ready: false,
};

const READY: CompanySetupReadiness = {
  runtime_online: true,
  secretary_id: "sec-1",
  executive_floor: true,
  autopilot_daily: true,
  autopilot_weekly_paused: true,
  ready: true,
};

const AGENTS = [{ id: "sec-1", name: "Secretary", runtime_id: "rt-1", archived_at: null }] as Agent[];

describe("CompanySetupCard", () => {
  let queryClient: QueryClient;
  let pinned: { agent_id: string; position: number }[];

  beforeEach(() => {
    vi.clearAllMocks();
    admin.value = true;
    pinned = [];
    api.listChatPinnedAgents.mockImplementation(async () => pinned);
    api.pinChatAgent.mockImplementation(async (agentId: string) => {
      pinned = [...pinned, { agent_id: agentId, position: pinned.length + 1 }];
      return pinned.at(-1);
    });
    api.listAgents.mockResolvedValue(AGENTS);
    api.listSquads.mockResolvedValue([]);
    api.listAutopilots.mockResolvedValue({ autopilots: [], total: 0 });
    api.listCompanyFloors.mockResolvedValue([]);
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(workspaceKeys.agents("ws-1"), AGENTS);
    queryClient.setQueryData(workspaceKeys.squads("ws-1"), [] as Squad[]);
    queryClient.setQueryData(chatKeys.pinnedAgents("ws-1"), pinned);
  });
  afterEach(cleanup);

  function setup(readiness: CompanySetupReadiness) {
    api.getCompanySetup.mockResolvedValue(readiness);
    queryClient.setQueryData(companySetupKeys.all("ws-1"), readiness);
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <CompanySetupCard />
      </QueryClientProvider>,
    );
  }

  it("shows the install guide and a retry when the Hermes runtime is missing", () => {
    setup({ ...NOT_SET_UP, runtime_online: false });
    expect(screen.getByText("Install Hermes on the machine that runs the Multica daemon, then retry.")).toBeTruthy();
    const guide = screen.getByRole("link", { name: /Install guide/ });
    expect(guide.getAttribute("href")).toMatch(/\/install-agent-runtime$/);
    expect(guide.getAttribute("target")).toBe("_blank");
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Set up" })).toBeNull();
  });

  it("lists the readiness checklist with the missing pieces marked", () => {
    setup(NOT_SET_UP);
    expect(screen.getByText("Hermes runtime online")).toBeTruthy();
    expect(screen.getByText("Executive floor").className).toContain("text-muted-foreground");
    expect(screen.getByRole("button", { name: "Set up" })).toBeTruthy();
  });

  it("offers the Market Research squad once the company is set up", () => {
    setup(READY);
    expect(screen.getByRole("button", { name: "Create the Market Research squad" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Set up" })).toBeNull();
  });

  it("hides the card for a member once the company is set up", () => {
    admin.value = false;
    setup(READY);
    expect(screen.queryByText("Set up your company")).toBeNull();
  });

  it("gives a member read-only status with no action", () => {
    admin.value = false;
    setup(NOT_SET_UP);
    expect(screen.getByText("An owner or admin finishes setup")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Set up" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("sets up the company and pins the Secretary once", async () => {
    api.setupCompany.mockResolvedValue(READY);
    setup(NOT_SET_UP);
    // The refetch that follows setup sees the finished company.
    api.getCompanySetup.mockResolvedValue(READY);
    fireEvent.click(screen.getByRole("button", { name: "Set up" }));
    await waitFor(() => expect(api.setupCompany).toHaveBeenCalledTimes(1));
    expect(api.setupCompany.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ secretary_instructions: expect.any(String) }),
    );
    await waitFor(() => expect(api.pinChatAgent).toHaveBeenCalledTimes(1));
    expect(api.pinChatAgent).toHaveBeenCalledWith("sec-1");
  });

  it("switches to the install guide when setup is refused for a missing runtime", async () => {
    api.setupCompany.mockRejectedValue(
      new ApiError("runtime required", 409, "Conflict", {
        code: "runtime_required",
        hint: "Install Hermes",
        docs_path: "/install-agent-runtime",
      }),
    );
    setup(NOT_SET_UP);
    fireEvent.click(screen.getByRole("button", { name: "Set up" }));
    expect(await screen.findByRole("link", { name: /Install guide/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Set up" })).toBeNull();
    expect(api.pinChatAgent).not.toHaveBeenCalled();
  });
});
