import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockAuthState = { user: { id: "user-1" }, isAuthenticated: true };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => (selector ? selector(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  ),
  registerAuthStore: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () =>
      Promise.resolve([
        { user_id: "user-1", name: "Test User", email: "t@t.com", role: "admin" },
      ]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
  squadListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "squads"],
    queryFn: () => Promise.resolve([]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "assignee-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => id }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: any) => <span data-testid="actor">{actorId}</span>,
}));

const selectionState = {
  selectedIds: new Set<string>(),
  clear: vi.fn(),
};

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: any) => selector(selectionState),
}));

const mockBatchUpdate = vi.fn();
const mockBatchDelete = vi.fn();
vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({
    mutateAsync: mockBatchUpdate,
    isPending: false,
  }),
  useBatchDeleteIssues: () => ({
    mutateAsync: mockBatchDelete,
    isPending: false,
  }),
}));

const mockOpenModal = vi.fn();
vi.mock("@multica/core/modals", () => ({
  useModalStore: (selector: any) => selector({ open: mockOpenModal }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { BatchActionToolbar } from "./batch-action-toolbar";

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  selectionState.selectedIds = new Set(["issue-1", "issue-2"]);
  selectionState.clear.mockReset();
  mockBatchUpdate.mockReset();
  mockBatchDelete.mockReset();
  mockOpenModal.mockReset();
});

describe("BatchActionToolbar", () => {
  it("opens a confirmation modal before batch updating status to cancelled", async () => {
    render(wrap(<BatchActionToolbar />));

    fireEvent.click(screen.getByText("Status"));
    fireEvent.click(await screen.findByText("Cancelled"));

    expect(mockBatchUpdate).not.toHaveBeenCalled();
    expect(mockOpenModal).toHaveBeenCalledWith("issue-status-confirm", {
      status: "cancelled",
      count: 2,
      onConfirm: expect.any(Function),
    });

    const payload = mockOpenModal.mock.calls.at(-1)?.[1] as {
      onConfirm: () => Promise<void>;
    };
    await payload.onConfirm();

    expect(mockBatchUpdate).toHaveBeenCalledWith({
      ids: ["issue-1", "issue-2"],
      updates: { status: "cancelled" },
    });
  });

  it("opens a confirmation modal before batch updating status to archive", async () => {
    render(wrap(<BatchActionToolbar />));

    fireEvent.click(screen.getByText("Status"));
    fireEvent.click(await screen.findByText("Archive"));

    expect(mockBatchUpdate).not.toHaveBeenCalled();
    expect(mockOpenModal).toHaveBeenCalledWith("issue-status-confirm", {
      status: "archive",
      count: 2,
      onConfirm: expect.any(Function),
    });
  });

  it("updates non-confirmable batch statuses immediately", async () => {
    render(wrap(<BatchActionToolbar />));

    fireEvent.click(screen.getByText("Status"));
    fireEvent.click(await screen.findByText("Done"));

    await waitFor(() => {
      expect(mockBatchUpdate).toHaveBeenCalledWith({
        ids: ["issue-1", "issue-2"],
        updates: { status: "done" },
      });
    });
    expect(mockOpenModal).not.toHaveBeenCalled();
  });
});
