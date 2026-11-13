import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { RenameIssueModal } from "./rename-issue";

const { mockUpdate, toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
  mockUpdate: vi.fn(),
  toastErrorMock: vi.fn(),
  toastSuccessMock: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { updateIssue: mockUpdate },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("sonner", () => ({
  toast: { success: toastSuccessMock, error: toastErrorMock },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (x: Record<string, Record<string, string>>) => string) =>
      sel({
        common: { cancel: "Cancel" },
        revision: { conflict: "The title changed. Compare before saving again." },
        rename_issue: {
          title: "Rename issue",
          placeholder: "Issue title",
          input_aria_label: "Rename {{identifier}}",
          submit: "Save",
          saving: "Saving...",
          title_required: "Enter a title",
          toast_renamed: "Issue renamed",
          toast_failed: "Failed to rename issue",
        },
      }),
  }),
}));

function renderModal(data: Record<string, unknown> | null) {
  const onClose = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(issueKeys.detail("ws-1", issue.id), issue);
  render(
    <QueryClientProvider client={queryClient}>
      <RenameIssueModal onClose={onClose} data={data} />
    </QueryClientProvider>,
  );
  return { onClose, input: screen.getByRole("textbox"), queryClient };
}

const issue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Old name",
  revision: 1,
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  labels: [],
  metadata: {},
  properties: {},
  created_at: "2026-10-19T00:00:00Z",
  updated_at: "2026-10-19T00:00:00Z",
};

const data = { issueId: "issue-1", identifier: "TES-1", title: "Old name" };

describe("RenameIssueModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdate.mockImplementation(async (_id: string, updates: UpdateIssueRequest) => ({
      ...issue,
      ...updates,
      revision: 2,
    }));
  });

  it("writes the trimmed name and closes on success", async () => {
    const { onClose, input } = renderModal(data);

    fireEvent.change(input, { target: { value: "  New name  " } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith("issue-1", {
        title: "New name",
        title_base: "Old name",
      }),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(toastSuccessMock).toHaveBeenCalledWith("Issue renamed");
  });

  // Submitting the unchanged name must not fire a pointless write; the user
  // opened the dialog, agreed with the existing title, and wants out.
  it("closes without a write when the name is unchanged", async () => {
    const { onClose } = renderModal(data);

    fireEvent.click(screen.getByText("Save"));

    expect(mockUpdate).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("refuses an empty name and keeps the dialog open", async () => {
    const { onClose, input } = renderModal(data);

    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.click(screen.getByText("Save"));

    expect(mockUpdate).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(toastErrorMock).toHaveBeenCalledWith("Enter a title");
  });

  it("keeps the dialog open when the write fails", async () => {
    mockUpdate.mockRejectedValueOnce(new Error("boom"));
    const { onClose, input } = renderModal(data);

    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledWith("boom"));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("protects a concurrent rename and preserves the draft and original baseline on retry", async () => {
    let serverIssue = { ...issue, title: "Someone else's name", revision: 2 };
    mockUpdate.mockImplementation(async (_id: string, updates: UpdateIssueRequest) => {
      if (updates.title_base !== undefined && updates.title_base !== serverIssue.title) {
        throw new ApiError("raw server conflict", 409, "Conflict", {
          code: "revision_conflict",
        });
      }
      serverIssue = { ...serverIssue, ...updates, revision: serverIssue.revision + 1 };
      return serverIssue;
    });
    const { onClose, input, queryClient } = renderModal(data);

    fireEvent.change(input, { target: { value: "My draft" } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() =>
      expect(toastErrorMock).toHaveBeenCalledWith("The title changed. Compare before saving again."),
    );
    expect(serverIssue.title).toBe("Someone else's name");
    expect(input).toHaveValue("My draft");
    expect(onClose).not.toHaveBeenCalled();
    expect(toastSuccessMock).not.toHaveBeenCalled();

    // A refreshed cache must not silently approve overwriting the remote edit.
    queryClient.setQueryData(issueKeys.detail("ws-1", issue.id), serverIssue);
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledTimes(2));
    expect(serverIssue.title).toBe("Someone else's name");
    expect(input).toHaveValue("My draft");
    expect(onClose).not.toHaveBeenCalled();
    expect(toastSuccessMock).not.toHaveBeenCalled();
  });
});
