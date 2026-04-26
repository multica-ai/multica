import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// Mock the auth store: return a fixed user.
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

// Mock the workspace id hook.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// Mock query options + mutations from the workspace package.
const mockBindings: Array<Record<string, unknown>> = [
  {
    id: "b-1",
    workspace_id: "ws-1",
    repo_full_name: "zeyad-farrag/multica",
    installation_id: 127217055,
    cr_bot_username: "coderabbitai[bot]",
    active: true,
    created_at: "2026-04-26T10:00:00.000Z",
    updated_at: "2026-04-26T10:00:00.000Z",
  },
];

const memberListMock = vi.fn(() => ({
  queryKey: ["workspaces", "ws-1", "members"],
  queryFn: async () => [
    { user_id: "user-1", role: "admin" },
  ],
}));

const repoBindingListMock = vi.fn(() => ({
  queryKey: ["workspaces", "ws-1", "repo-bindings"],
  queryFn: async () => mockBindings,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (_wsId: string) => memberListMock(),
  repoBindingListOptions: (_wsId: string) => repoBindingListMock(),
}));

const createMutateAsync = vi.fn();
const updateMutateAsync = vi.fn();
const deleteMutateAsync = vi.fn();

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateRepoBinding: () => ({
    mutateAsync: createMutateAsync,
    isPending: false,
  }),
  useUpdateRepoBinding: () => ({
    mutateAsync: updateMutateAsync,
    isPending: false,
  }),
  useDeleteRepoBinding: () => ({
    mutateAsync: deleteMutateAsync,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { PRAutomationTab } from "./pr-automation-tab";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("PRAutomationTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    createMutateAsync.mockReset();
    updateMutateAsync.mockReset();
    deleteMutateAsync.mockReset();
  });

  it("renders existing bindings", async () => {
    render(wrap(<PRAutomationTab />));
    await waitFor(() => {
      expect(screen.getByText("zeyad-farrag/multica")).toBeInTheDocument();
    });
    // Installation + bot username are shown
    expect(screen.getByText(/Installation 127217055/)).toBeInTheDocument();
    expect(screen.getByText(/coderabbitai\[bot\]/)).toBeInTheDocument();
  });

  it("rejects invalid repo format on create", async () => {
    const user = userEvent.setup();
    render(wrap(<PRAutomationTab />));
    await screen.findByText("zeyad-farrag/multica");

    await user.click(screen.getByRole("button", { name: /Add binding/i }));
    await user.type(
      screen.getByPlaceholderText(/owner\/repo/i),
      "not-valid-name",
    );
    await user.type(
      screen.getByPlaceholderText(/installation ID/i),
      "12345",
    );
    await user.click(screen.getByRole("button", { name: /Add binding/i }));

    expect(createMutateAsync).not.toHaveBeenCalled();
  });

  it("calls createRepoBinding with valid input", async () => {
    createMutateAsync.mockResolvedValue({ id: "b-2" });
    const user = userEvent.setup();
    render(wrap(<PRAutomationTab />));
    await screen.findByText("zeyad-farrag/multica");

    await user.click(screen.getByRole("button", { name: /Add binding/i }));
    await user.type(
      screen.getByPlaceholderText(/owner\/repo/i),
      "acme/widgets",
    );
    await user.type(
      screen.getByPlaceholderText(/installation ID/i),
      "999",
    );
    await user.click(screen.getByRole("button", { name: /Add binding/i }));

    await waitFor(() => {
      expect(createMutateAsync).toHaveBeenCalledWith({
        repo_full_name: "acme/widgets",
        installation_id: 999,
        cr_bot_username: undefined,
      });
    });
  });

  it("rejects non-positive installation id", async () => {
    const user = userEvent.setup();
    render(wrap(<PRAutomationTab />));
    await screen.findByText("zeyad-farrag/multica");

    await user.click(screen.getByRole("button", { name: /Add binding/i }));
    await user.type(
      screen.getByPlaceholderText(/owner\/repo/i),
      "acme/widgets",
    );
    await user.type(
      screen.getByPlaceholderText(/installation ID/i),
      "-5",
    );
    await user.click(screen.getByRole("button", { name: /Add binding/i }));

    expect(createMutateAsync).not.toHaveBeenCalled();
  });
});
