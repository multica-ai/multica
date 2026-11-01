// @vitest-environment jsdom

import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import type { QuickstartOutcome } from "@multica/core/squads";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import enAgents from "../locales/en/agents.json";

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, agents: enAgents },
};

const mocks = vi.hoisted(() => ({
  agents: [] as Agent[],
  mutateAsync: vi.fn(),
  isPending: false,
  navigationPush: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (key.includes("agents")) return { data: mocks.agents };
    if (key.includes("runtimes")) {
      return {
        data: [{ id: "rt-1", owner_id: "user-me", status: "online", visibility: "private" }],
        isLoading: false,
      };
    }
    return { data: [] };
  },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  isRuntimeUsableForUser: () => true,
}));

vi.mock("@multica/core/squads", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/squads")>(
    "@multica/core/squads",
  );
  return {
    ...actual,
    useSquadQuickstart: () => ({
      mutateAsync: mocks.mutateAsync,
      get isPending() {
        return mocks.isPending;
      },
    }),
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "user-me" } }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ squadDetail: (id: string) => `/ws/squads/${id}` }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: mocks.navigationPush }) }));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));
vi.mock("../agents/components/runtime-picker", () => ({
  RuntimePicker: ({ onSelect }: { onSelect: (id: string) => void }) => (
    <button type="button" onClick={() => onSelect("rt-1")}>
      pick runtime
    </button>
  ),
}));

import { SquadQuickstartModal } from "./squad-quickstart";

function renderModal() {
  const onClose = vi.fn();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
  render(<SquadQuickstartModal onClose={onClose} />, { wrapper });
  return { onClose };
}

const readyOutcome: QuickstartOutcome = {
  agents: [{ role: "lead", name: "SDLC Lead", id: "a1", status: "created" }],
  squad: { id: "squad-9", status: "created" },
  workflow: { status: "created" },
  routine: { status: "skipped" },
};

describe("SquadQuickstartModal", () => {
  beforeEach(() => {
    mocks.agents = [];
    mocks.isPending = false;
    mocks.mutateAsync.mockReset();
  });
  afterEach(cleanup);

  it("lists both templates and the agents the first one needs", () => {
    renderModal();

    expect(screen.getByRole("radio", { name: /SDLC · New product/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: /SDLC · Existing product/ })).not.toBeChecked();
    expect(screen.getByText("SDLC Lead")).toBeInTheDocument();
    expect(screen.getAllByText("New").length).toBeGreaterThan(0);
  });

  it("needs a runtime before it can create agents", async () => {
    renderModal();
    const user = userEvent.setup();

    expect(screen.getByRole("button", { name: "Create squad" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "pick runtime" }));

    expect(screen.getByRole("button", { name: "Create squad" })).toBeEnabled();
  });

  it("creates the greenfield squad with its workflow on the chosen runtime", async () => {
    mocks.mutateAsync.mockResolvedValue(readyOutcome);
    renderModal();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "pick runtime" }));
    await user.click(screen.getByRole("button", { name: "Create squad" }));

    const arg = mocks.mutateAsync.mock.calls[0]![0];
    expect(arg.template.id).toBe("sdlc_greenfield");
    expect(arg.options).toEqual({ runtimeId: "rt-1", createWorkflow: true });
    expect(await screen.findByText("Squad ready")).toBeInTheDocument();
    expect(mocks.toastSuccess).toHaveBeenCalled();
  });

  it("leaves the workflow out when its box is unticked", async () => {
    mocks.mutateAsync.mockResolvedValue(readyOutcome);
    renderModal();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "pick runtime" }));
    await user.click(screen.getByRole("checkbox", { name: /SDLC Auto/ }));
    await user.click(screen.getByRole("button", { name: "Create squad" }));

    expect(mocks.mutateAsync.mock.calls[0]![0].options.createWorkflow).toBe(false);
  });

  it("offers no workflow for the maintenance squad", async () => {
    renderModal();

    await userEvent.setup().click(screen.getByRole("radio", { name: /SDLC · Existing product/ }));

    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("asks for no runtime when every agent already exists", async () => {
    const { SQUAD_ROLES } = await import("@multica/core/squads");
    mocks.agents = Object.values(SQUAD_ROLES).map(
      (r) => ({ id: r.key, name: r.name, archived_at: null }) as unknown as Agent,
    );
    renderModal();

    expect(screen.queryByRole("button", { name: "pick runtime" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create squad" })).toBeEnabled();
  });

  it("shows what failed and offers to try again", async () => {
    mocks.mutateAsync.mockResolvedValueOnce({
      agents: [
        { role: "lead", name: "SDLC Lead", id: "a1", status: "created" },
        { role: "ux", name: "SDLC UX Designer", id: null, status: "failed", error: "runtime offline" },
      ],
      squad: { id: "squad-9", status: "created" },
      workflow: { status: "skipped" },
      routine: { status: "skipped" },
    } satisfies QuickstartOutcome);
    renderModal();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "pick runtime" }));
    await user.click(screen.getByRole("button", { name: "Create squad" }));

    expect(await screen.findByText("Finished with problems")).toBeInTheDocument();
    expect(screen.getByText("Failed: runtime offline")).toBeInTheDocument();
    mocks.mutateAsync.mockResolvedValueOnce(readyOutcome);
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(mocks.mutateAsync).toHaveBeenCalledTimes(2));
  });

  it("opens the new squad", async () => {
    mocks.mutateAsync.mockResolvedValue(readyOutcome);
    const { onClose } = renderModal();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "pick runtime" }));
    await user.click(screen.getByRole("button", { name: "Create squad" }));
    await user.click(await screen.findByRole("button", { name: "Open squad" }));

    expect(onClose).toHaveBeenCalled();
    expect(mocks.navigationPush).toHaveBeenCalledWith("/ws/squads/squad-9");
  });

  it("says when the QA step was left out", async () => {
    mocks.mutateAsync.mockResolvedValue({
      ...readyOutcome,
      workflow: { status: "created", note: "qa_step_left_out" },
    } satisfies QuickstartOutcome);
    renderModal();
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "pick runtime" }));
    await user.click(screen.getByRole("button", { name: "Create squad" }));

    expect(await screen.findByText(/Created without a QA step/)).toBeInTheDocument();
  });
});
