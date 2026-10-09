import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { QuickActionsTab } from "./quick-actions-tab";
import type { QuickAction } from "@multica/core/types";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

let actions: QuickAction[] = [];
const agents: unknown[] = [];
const skills: unknown[] = [];

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    if (options.queryKey[0] === "quick-actions") return { data: actions, isLoading: false };
    if (options.queryKey[0] === "agents") return { data: agents, isLoading: false };
    if (options.queryKey[0] === "skills") return { data: skills, isLoading: false };
    return { data: [], isLoading: false };
  },
}));

vi.mock("@multica/core/quick-actions", () => ({
  quickActionListOptions: (wsId: string) => ({ queryKey: ["quick-actions", wsId] }),
  useCreateQuickAction: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpdateQuickAction: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDeleteQuickAction: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/agents", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
}));

vi.mock("@multica/core/skills", () => ({
  skillListOptions: (wsId: string) => ({ queryKey: ["skills", wsId] }),
}));

vi.mock("../../autopilots/components/pickers/agent-picker", () => ({
  AgentPicker: () => <div data-testid="agent-picker">AgentPicker</div>,
}));

describe("QuickActionsTab", () => {
  afterEach(() => {
    cleanup();
    actions = [];
  });

  it("renders empty state and allows opening the create dialog with bounded layout", async () => {
    const user = userEvent.setup();
    renderWithI18n(<QuickActionsTab />);

    expect(screen.getByRole("button", { name: /New quick action/i })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /New quick action/i }));

    // Verify dialog title is displayed
    expect(screen.getByRole("heading", { name: /New quick action/i })).toBeInTheDocument();

    // Verify prompt textarea has bounded max height and scroll classes (MUL-5465, fixes #8587)
    const promptTextarea = screen.getByLabelText(/Prompt/i);
    expect(promptTextarea).toBeInTheDocument();
    expect(promptTextarea.className).toContain("max-h-60");
    expect(promptTextarea.className).toContain("overflow-y-auto");

    // Verify dialog content has max-height and flex-col to prevent viewport overflow
    const dialogContent = promptTextarea.closest("[data-slot='dialog-content']");
    expect(dialogContent).not.toBeNull();
    expect(dialogContent?.className).toContain("max-h-[85dvh]");
    expect(dialogContent?.className).toContain("flex-col");

    // Verify form fields container is scrollable with overflow-y-auto
    const formContainer = promptTextarea.closest(".overflow-y-auto");
    expect(formContainer).not.toBeNull();
  });
});
