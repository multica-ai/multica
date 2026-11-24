import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { DeleteIssueConfirmModal } from "./delete-issue-confirm";
import { NavigationProvider } from "../navigation";
import type { NavigationAdapter } from "../navigation";

const mockDelete = vi.fn().mockResolvedValue(undefined);
vi.mock("@multica/core/issues/mutations", () => ({
  useDeleteIssue: () => ({ mutateAsync: mockDelete }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (x: Record<string, Record<string, string>>) => string) =>
      sel({
        delete_issue: {
          title: "Delete issue?",
          description: "This cannot be undone.",
          hint: "Sub-issues are deleted too.",
          cancel: "Cancel",
          confirm: "Delete",
          deleting: "Deleting...",
          toast_deleted: "Issue deleted",
          toast_delete_failed: "Delete failed",
        },
      }),
  }),
}));

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues/issue-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

async function deleteWith(
  adapter: NavigationAdapter,
  data: Record<string, unknown> | null,
) {
  const onClose = vi.fn();
  render(
    <NavigationProvider value={adapter}>
      <DeleteIssueConfirmModal onClose={onClose} data={data} />
    </NavigationProvider>,
  );
  fireEvent.click(screen.getByText("Delete"));
  await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("issue-1"));
  return onClose;
}

describe("DeleteIssueConfirmModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The bug this guards (GH #5995): deleting from an issue opened out of My
  // Issues used to hard-push the workspace Issues list, dropping the user's
  // navigation context. The same applied to project lists, search and pins.
  it("returns to the list the issue was opened from", async () => {
    const adapter = makeAdapter({ canGoBack: () => true });

    await deleteWith(adapter, {
      issueId: "issue-1",
      onDeletedFallbackPath: "/acme/issues",
    });

    await waitFor(() => expect(adapter.back).toHaveBeenCalledTimes(1));
    expect(adapter.replace).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("falls back to the workspace list when the issue was opened cold", async () => {
    const adapter = makeAdapter({ canGoBack: () => false });

    await deleteWith(adapter, {
      issueId: "issue-1",
      onDeletedFallbackPath: "/acme/issues",
    });

    await waitFor(() =>
      expect(adapter.replace).toHaveBeenCalledWith("/acme/issues"),
    );
    expect(adapter.back).not.toHaveBeenCalled();
  });

  // List surfaces delete in place (row context menu, batch toolbar): there is
  // no page to leave, so the modal must not navigate at all.
  it("does not navigate when no fallback path is supplied", async () => {
    const adapter = makeAdapter({ canGoBack: () => true });

    const onClose = await deleteWith(adapter, { issueId: "issue-1" });

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(adapter.back).not.toHaveBeenCalled();
    expect(adapter.replace).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("stays put when the delete fails", async () => {
    mockDelete.mockRejectedValueOnce(new Error("nope"));
    const adapter = makeAdapter({ canGoBack: () => true });

    render(
      <NavigationProvider value={adapter}>
        <DeleteIssueConfirmModal
          onClose={vi.fn()}
          data={{ issueId: "issue-1", onDeletedFallbackPath: "/acme/issues" }}
        />
      </NavigationProvider>,
    );
    fireEvent.click(screen.getByText("Delete"));

    await waitFor(() => expect(screen.getByText("Delete")).toBeInTheDocument());
    expect(adapter.back).not.toHaveBeenCalled();
    expect(adapter.replace).not.toHaveBeenCalled();
  });
});
