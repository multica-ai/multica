import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { chatKeys } from "@multica/core/chat/queries";
import type { Agent } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimesPage } from "./runtimes-page";

const mocks = vi.hoisted(() => ({
  api: {
    listAgents: vi.fn(),
    listChatSessions: vi.fn(),
    listRuntimes: vi.fn(),
    listRuntimeProfiles: vi.fn(),
    getAgentTaskSnapshot: vi.fn(),
    createMikaAgent: vi.fn(),
    startMikaOnboarding: vi.fn(),
  },
  push: vi.fn(),
  error: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: mocks.api }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => {
  const state = { isLoading: false, user: { id: "member-1" } };
  return {
    useAuthStore: Object.assign(
      (selector: (value: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/paths", () => ({
  useRequiredWorkspaceSlug: () => "workspace",
  useWorkspacePaths: () => ({
    chatSession: (id: string) => `/workspace/chat/${id}`,
  }),
}));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {} }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: mocks.push }),
  AppLink: (props: ComponentProps<"a">) => <a {...props} />,
}));
vi.mock("sonner", () => ({ toast: { error: mocks.error } }));
vi.mock("./connect-remote-dialog", () => ({ ConnectRemoteDialog: () => null }));
vi.mock("./cloud-runtime-dialog", () => ({ CloudRuntimeDialog: () => null }));
vi.mock("./runtime-list", () => ({
  buildWorkloadIndex: () => new Map(),
  RuntimeList: () => null,
}));
vi.mock("./runtime-machines", () => ({ buildRuntimeMachines: () => [] }));
vi.mock("./mika-runtime-choice", () => ({
  MikaRuntimeChoice: () => <div>Runtime choice</div>,
}));

const mika = {
  id: "mika-1",
  system_key: "mika",
  name: "Mika",
  runtime_id: "configured-runtime",
  model: "configured-model",
} as Agent;
const session = { id: "session-1", agent_id: mika.id, status: "active" };

beforeEach(() => {
  vi.resetAllMocks();
  mocks.api.listAgents.mockResolvedValue([mika]);
  mocks.api.listChatSessions.mockResolvedValue([]);
  mocks.api.listRuntimes.mockResolvedValue([
    {
      id: "other-runtime",
      name: "Other computer",
      provider: "codex",
      status: "online",
    },
  ]);
  mocks.api.listRuntimeProfiles.mockResolvedValue([]);
  mocks.api.getAgentTaskSnapshot.mockResolvedValue([]);
  mocks.api.createMikaAgent.mockResolvedValue({
    ...mika,
    onboarding_session: session,
  });
  mocks.api.startMikaOnboarding.mockResolvedValue({
    started: true,
    message_id: "opening-1",
  });
});

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider
        locale="en"
        resources={{ en: { common: enCommon, runtimes: enRuntimes } }}
      >
        <RuntimesPage hasLocalMachine />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return qc;
}

// Session-history permutations are covered by core/onboarding/mika.test.ts.
// This suite covers real query transitions and the recovery action's wiring.
describe("Mika recovery on the Runtimes page", () => {
  it.each(["agents", "sessions"] as const)(
    "does not offer setup when the %s query fails, and can retry",
    async (failed) => {
      const request =
        failed === "agents" ? mocks.api.listAgents : mocks.api.listChatSessions;
      request.mockRejectedValue(new Error("Network unavailable"));
      const qc = renderPage();
      const key =
        failed === "agents"
          ? workspaceKeys.agents("ws-1")
          : chatKeys.sessions("ws-1");
      await waitFor(() => expect(qc.getQueryState(key)?.status).toBe("error"));
      expect(
        screen.queryByRole("button", { name: "Start with Mika" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Continue setup" }),
      ).not.toBeInTheDocument();
      request.mockResolvedValue(failed === "agents" ? [mika] : []);
      await userEvent.click(
        await screen.findByRole("button", { name: "Retry" }),
      );
      expect(
        await screen.findByRole("button", { name: "Continue setup" }),
      ).toBeInTheDocument();
      expect(mocks.api.createMikaAgent).not.toHaveBeenCalled();
    },
  );

  it("resumes with the existing agent's runtime without opening a runtime picker", async () => {
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Continue setup" }),
    );
    await waitFor(() =>
      expect(mocks.push).toHaveBeenCalledWith("/workspace/chat/session-1"),
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.api.createMikaAgent).toHaveBeenCalledWith(
      expect.objectContaining({
        runtime_id: "configured-runtime",
        model: undefined,
      }),
      "workspace",
    );
    expect(mocks.api.startMikaOnboarding).toHaveBeenCalledWith(
      "session-1",
      { language: "en" },
      "workspace",
    );
  });

  it("keeps recovery available after the opening fails and retries the same server-resolved session", async () => {
    mocks.api.startMikaOnboarding.mockRejectedValueOnce(
      new Error("Opening failed"),
    );
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Continue setup" }),
    );
    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("Opening failed"),
    );
    expect(mocks.push).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Continue setup" }),
    );
    await waitFor(() =>
      expect(mocks.push).toHaveBeenCalledWith("/workspace/chat/session-1"),
    );
    expect(mocks.api.startMikaOnboarding.mock.calls.map(([id]) => id)).toEqual([
      "session-1",
      "session-1",
    ]);
  });

  it("refreshes partial provisioning after failure even without an agent-created event", async () => {
    mocks.api.listAgents.mockResolvedValue([]);
    mocks.api.createMikaAgent.mockImplementationOnce(async () => {
      // The agent committed, but opening the member's session failed. The
      // websocket event may be missed while the client is reconnecting.
      mocks.api.listAgents.mockResolvedValue([mika]);
      throw new Error("Could not open the conversation");
    });
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Start with Mika" }),
    );
    await userEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Start with Mika",
      }),
    );
    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith(
        "Could not open the conversation",
      ),
    );
    expect(
      await screen.findByRole("button", { name: "Continue setup" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mocks.api.startMikaOnboarding).not.toHaveBeenCalled();
    await userEvent.click(
      screen.getByRole("button", { name: "Continue setup" }),
    );
    await waitFor(() =>
      expect(mocks.push).toHaveBeenCalledWith("/workspace/chat/session-1"),
    );
  });

  it("asks for a runtime only when Mika has not been provisioned", async () => {
    mocks.api.listAgents.mockResolvedValue([]);
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Start with Mika" }),
    );
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("Runtime choice")).toBeInTheDocument();
    expect(mocks.api.createMikaAgent).not.toHaveBeenCalled();
    await userEvent.click(
      within(dialog).getByRole("button", { name: "Start with Mika" }),
    );
    await waitFor(() =>
      expect(mocks.api.createMikaAgent).toHaveBeenCalledWith(
        expect.objectContaining({ runtime_id: "other-runtime" }),
        "workspace",
      ),
    );
  });

  it("does not silently select another runtime when the existing agent's binding is missing", async () => {
    mocks.api.listAgents.mockResolvedValue([{ ...mika, runtime_id: "" }]);
    renderPage();
    await userEvent.click(
      await screen.findByRole("button", { name: "Continue setup" }),
    );
    expect(mocks.error).toHaveBeenCalled();
    expect(mocks.api.createMikaAgent).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
