import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { DeleteIssueConfirmModal } from "./delete-issue-confirm";
import { NavigationProvider } from "../navigation";
import type { NavigationAdapter } from "../navigation";

const mockDelete = vi.fn().mockResolvedValue(undefined);
vi.mock("@multica/core/issues/mutations", () => ({
  useDeleteIssue: () => ({ mutateAsync: mockDelete }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));

// Workspace-wide child-progress map, warm exactly as it is on every surface
// that can open this dialog. `childProgress` drives the "sub-issues become
// standalone" warning; an empty map stands in for a surface that never loaded
// it, where the dialog must stay silent rather than claim zero.
const childProgress = new Map<string, { done: number; total: number }>();
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) =>
    queryKey[0] === "child-progress" ? { data: childProgress } : { data: undefined },
}));
vi.mock("@multica/core/issues/queries", () => ({
  childIssueProgressOptions: () => ({ queryKey: ["child-progress"] }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      sel: (x: Record<string, Record<string, string>>) => string,
      vars?: Record<string, unknown>,
    ) => {
      const raw = sel({
        delete_issue: {
          title: "Delete this issue?",
          title_named: "Delete {{identifier}}?",
          description: "Comments and attachments go with it.",
          sub_issues_detached: "{{count}} sub-issues become standalone.",
          cancel: "Cancel",
          confirm: "Delete",
          deleting: "Deleting...",
          toast_deleted: "Issue deleted",
          toast_delete_failed: "Delete failed",
        },
      });
      return raw.replace(/\{\{(\w+)\}\}/g, (_m, k) => String(vars?.[k] ?? ""));
    },
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
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderModal(
  adapter: NavigationAdapter,
  data: Record<string, unknown> | null,
) {
  const onClose = vi.fn();
  render(
    <NavigationProvider value={adapter}>
      <DeleteIssueConfirmModal onClose={onClose} data={data} />
    </NavigationProvider>,
  );
  return onClose;
}

async function deleteWith(
  adapter: NavigationAdapter,
  data: Record<string, unknown> | null,
) {
  const onClose = renderModal(adapter, data);
  fireEvent.click(screen.getByText("Delete"));
  await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("issue-1"));
  return onClose;
}

describe("DeleteIssueConfirmModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    childProgress.clear();
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

    // The adapter under assertion must be the one the component got: rendering
    // a second `makeAdapter()` here left the assertions below pointing at an
    // object no component ever touched, so they passed no matter what the
    // failure path did.
    renderModal(adapter, {
      issueId: "issue-1",
      onDeletedFallbackPath: "/acme/issues",
    });
    fireEvent.click(screen.getByText("Delete"));

    await waitFor(() => expect(screen.getByText("Delete")).toBeInTheDocument());
    expect(adapter.back).not.toHaveBeenCalled();
    expect(adapter.replace).not.toHaveBeenCalled();
  });

  // Opened as often from a row's context menu as from the issue's own page,
  // where a generic heading gives the user nothing to check their aim against
  // before a permanent delete. It goes in the title so naming the subject
  // costs no extra line of prose.
  it("names the issue being deleted in the title", () => {
    renderModal(makeAdapter(), { issueId: "issue-1", identifier: "TST-1" });

    expect(screen.getByText("Delete TST-1?")).toBeInTheDocument();
  });

  it("falls back to the unnamed title when no identifier came through", () => {
    renderModal(makeAdapter(), { issueId: "issue-1" });

    expect(screen.getByText("Delete this issue?")).toBeInTheDocument();
  });

  // The server clears the children's parent link rather than deleting them, so
  // a parent delete quietly promotes every sub-issue to top level.
  it("warns that sub-issues survive as standalone issues", () => {
    childProgress.set("issue-1", { done: 1, total: 3 });

    renderModal(makeAdapter(), { issueId: "issue-1", identifier: "TST-1" });

    expect(
      screen.getByText("3 sub-issues become standalone."),
    ).toBeInTheDocument();
  });

  it("stays silent about sub-issues when the issue has none", () => {
    childProgress.set("other-issue", { done: 0, total: 2 });

    renderModal(makeAdapter(), { issueId: "issue-1", identifier: "TST-1" });

    expect(screen.queryByText(/become standalone/)).not.toBeInTheDocument();
  });

  // `AlertDialogAction` is a plain Button here — it does not close the dialog
  // on click — so the pending state is the only thing standing between an
  // impatient second click and a second DELETE.
  it("does not fire a second delete while the first is in flight", async () => {
    let resolveDelete!: () => void;
    mockDelete.mockImplementationOnce(
      () => new Promise<void>((res) => { resolveDelete = () => res(); }),
    );

    renderModal(makeAdapter(), { issueId: "issue-1", identifier: "TST-1" });
    fireEvent.click(screen.getByText("Delete"));

    await waitFor(() => expect(screen.getByText("Deleting...")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Deleting..."));

    expect(mockDelete).toHaveBeenCalledTimes(1);
    resolveDelete();
  });
});
