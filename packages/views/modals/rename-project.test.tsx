import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, screen, fireEvent, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Project, UpdateProjectRequest } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { RenameProjectModal } from "./rename-project";

const { mockUpdate, toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
  mockUpdate: vi.fn(),
  toastErrorMock: vi.fn(),
  toastSuccessMock: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { updateProject: mockUpdate },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("sonner", () => ({
  toast: { success: toastSuccessMock, error: toastErrorMock },
}));

const project: Project = {
  id: "project-1",
  workspace_id: "ws-1",
  title: "Old name",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};

const data = { projectId: "project-1", title: "Old name" };

function renderModal(modalData: Record<string, unknown> | null = data) {
  const onClose = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <RenameProjectModal onClose={onClose} data={modalData} />
    </QueryClientProvider>,
  );
  return { onClose, queryClient, input: screen.getByRole("textbox", { name: "Rename project" }) };
}

describe("RenameProjectModal", () => {
  beforeEach(() => {
    onlineManager.setOnline(true);
    vi.clearAllMocks();
    mockUpdate.mockImplementation(async (_id: string, updates: UpdateProjectRequest) => ({
      ...project,
      ...updates,
    }));
  });

  afterEach(() => {
    onlineManager.setOnline(true);
  });

  it("opens with the current name selected", async () => {
    const { input } = renderModal();

    expect(screen.getByRole("dialog", { name: "Rename project" })).toBeInTheDocument();
    expect(input).toHaveValue("Old name");
    await waitFor(() => expect(input).toHaveFocus());
    expect((input as HTMLInputElement).selectionStart).toBe(0);
    expect((input as HTMLInputElement).selectionEnd).toBe("Old name".length);
  });

  it("writes the trimmed name and closes on success", async () => {
    const { onClose, input } = renderModal();

    fireEvent.change(input, { target: { value: "  New name  " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith("project-1", { title: "New name" }),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(toastSuccessMock).toHaveBeenCalledWith("Project renamed");
  });

  it("submits with Enter", async () => {
    const { onClose, input } = renderModal();

    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.submit(input);

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith("project-1", { title: "New name" }),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  // Saving the unchanged name must not send a pointless write.
  it("closes without a write when the name is unchanged", () => {
    const { onClose } = renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(mockUpdate).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it("refuses an empty name and keeps the dialog open", () => {
    const { onClose, input } = renderModal();

    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(mockUpdate).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(toastErrorMock).toHaveBeenCalledWith("Enter a title");
  });

  it("keeps the dialog and the draft when the write fails", async () => {
    mockUpdate.mockRejectedValueOnce(new Error("boom"));
    const { onClose, input } = renderModal();

    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledWith("boom"));
    expect(onClose).not.toHaveBeenCalled();
    expect(input).toHaveValue("New name");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it.each(["Cancel", "Close", "Escape"])(
    "allows %s after an offline save without renaming on reconnect",
    async (dismiss) => {
      const user = userEvent.setup();
      const { onClose, input, queryClient } = renderModal();
      onlineManager.setOnline(false);

      fireEvent.change(input, { target: { value: "New name" } });
      fireEvent.click(screen.getByRole("button", { name: "Save" }));

      await waitFor(() =>
        expect(toastErrorMock).toHaveBeenCalledWith("You're offline. Reconnect and try renaming again."),
      );
      expect(input).toHaveValue("New name");
      expect(input).toBeEnabled();
      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
      expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
      expect(onClose).not.toHaveBeenCalled();
      expect(mockUpdate).not.toHaveBeenCalled();

      if (dismiss === "Escape") await user.keyboard("{Escape}");
      else await user.click(screen.getByRole("button", { name: dismiss }));
      expect(onClose).toHaveBeenCalledOnce();

      await act(async () => {
        onlineManager.setOnline(true);
        await queryClient.resumePausedMutations();
      });
      expect(mockUpdate).not.toHaveBeenCalled();
    },
  );

  it("requires an explicit retry after reconnecting and saves the retained draft", async () => {
    const { onClose, input, queryClient } = renderModal();
    onlineManager.setOnline(false);
    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(toastErrorMock).toHaveBeenCalledWith("You're offline. Reconnect and try renaming again."),
    );
    await act(async () => {
      onlineManager.setOnline(true);
      await queryClient.resumePausedMutations();
    });
    expect(mockUpdate).not.toHaveBeenCalled();
    expect(input).toHaveValue("New name");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(mockUpdate).toHaveBeenCalledExactlyOnceWith("project-1", { title: "New name" });
    expect(toastSuccessMock).toHaveBeenCalledWith("Project renamed");
  });

  it("closes from Cancel without a write", () => {
    const { onClose, input } = renderModal();

    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mockUpdate).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });
});
