import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { RenameIssueModal } from "./rename-issue";

const mockUpdate = vi.fn().mockResolvedValue({});

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: mockUpdate, isPending: false }),
}));

const { toastErrorMock, toastSuccessMock } = vi.hoisted(() => ({
  toastErrorMock: vi.fn(),
  toastSuccessMock: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: toastSuccessMock, error: toastErrorMock },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (x: Record<string, Record<string, string>>) => string) =>
      sel({
        common: { cancel: "Cancel" },
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
  render(<RenameIssueModal onClose={onClose} data={data} />);
  return { onClose, input: screen.getByRole("textbox") };
}

const data = { issueId: "issue-1", identifier: "TES-1", title: "Old name" };

describe("RenameIssueModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdate.mockResolvedValue({});
  });

  it("writes the trimmed name and closes on success", async () => {
    const { onClose, input } = renderModal(data);

    fireEvent.change(input, { target: { value: "  New name  " } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() =>
      expect(mockUpdate).toHaveBeenCalledWith({
        id: "issue-1",
        title: "New name",
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
});
