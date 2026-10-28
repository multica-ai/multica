import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { QuickAction } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { QuickActionsSection } from "./quick-actions-section";

// The section's contract (MUL-5465) is mostly about being HONEST with the
// user, so these tests target exactly that: what gets offered, and what the
// result claims happened.
//
// Two cases are worth locking hardest:
//   - `coalesced` — the DB allows one pending task per (issue, agent), so a
//     click can legitimately start NO new run. Reporting that as success is
//     the bug this feature is most likely to ship with.
//   - a permission refusal — the list is deliberately NOT filtered by invoke
//     permission, so a member CAN click something they cannot run. That must
//     open the explanatory dialog, not a bare error toast.

const runMock = vi.fn();
let listData: QuickAction[] = [];
let loading = false;
let queryError = false;
let workspaceId = "ws-1";
const refetch = vi.fn();

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: listData, isLoading: loading, isError: queryError, refetch }),
}));

const settingsPush = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: settingsPush }) }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
  useCurrentWorkspace: () => workspaceId ? ({ id: workspaceId }) : null,
}));

vi.mock("@multica/core/quick-actions", () => ({
  quickActionListOptions: () => ({ queryKey: ["quick-actions"], queryFn: vi.fn() }),
  useRunQuickAction: () => ({ mutateAsync: runMock, isPending: false }),
}));

let reasonCode: string | undefined;
vi.mock("@multica/core/api", () => ({
  dispatchReasonCode: () => reasonCode,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

const toastCalls: { kind: string; message: string }[] = [];
vi.mock("sonner", () => ({
  toast: {
    success: (m: string) => toastCalls.push({ kind: "success", message: m }),
    info: (m: string) => toastCalls.push({ kind: "info", message: m }),
    error: (m: string) => toastCalls.push({ kind: "error", message: m }),
  },
}));

function action(overrides: Partial<QuickAction> = {}): QuickAction {
  return {
    id: "qa-1",
    workspace_id: "ws-1",
    name: "Code Review",
    description: "",
    assignee_type: "agent",
    assignee_id: "agent-1",
    prompt: "review it",
    visibility: "public",
    status: "active",
    last_used_at: null,
    use_count: 0,
    created_by_id: "u-1",
    created_at: "",
    updated_at: "",
    target_name: "Lambda",
    target_public: true,
    target_missing: false,
    ...overrides,
  };
}

function outcome(status: string) {
  return {
    id: "c-1",
    trigger_outcomes: [
      { target_type: "agent", target_id: "agent-1", status, reason_code: status },
    ],
  };
}

describe("QuickActionsSection", () => {
  beforeEach(() => {
    runMock.mockReset();
    loading = false;
    queryError = false;
    workspaceId = "ws-1";
    refetch.mockReset();
    toastCalls.length = 0;
    reasonCode = undefined;
    listData = [action()];
  });

  it("hides during loading and outside a workspace", () => {
    loading = true;
    const view = renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(screen.queryByText("Quick Actions")).not.toBeInTheDocument();
    loading = false;
    workspaceId = "";
    view.rerender(<QuickActionsSection issueId="i-1" />);
    expect(screen.queryByText("Quick Actions")).not.toBeInTheDocument();
  });

  it("retries a failed catalog query and removes recovery after success", () => {
    listData = [];
    queryError = true;
    const view = renderWithI18n(<QuickActionsSection issueId="i-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Could not load. Retry" }));
    expect(refetch).toHaveBeenCalledOnce();
    queryError = false;
    listData = [action()];
    view.rerender(<QuickActionsSection issueId="i-1" />);
    expect(screen.queryByRole("button", { name: "Could not load. Retry" })).not.toBeInTheDocument();
    expect(screen.getByText("Code Review")).toBeVisible();
  });

  it("keeps the header free of create and settings buttons", () => {
    renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(screen.getByRole("button", { name: "Quick Actions 1" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "New quick action" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Quick Actions options" })).not.toBeInTheDocument();
  });

  it("renders nothing before the workspace has any actions", () => {
    listData = [];
    renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(screen.queryByText("Quick Actions")).not.toBeInTheDocument();
  });

  it("hides archived actions even if the server returned them", () => {
    listData = [action({ status: "archived" })];
    renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(screen.queryByText("Quick Actions")).not.toBeInTheDocument();
    expect(screen.queryByText("Code Review")).not.toBeInTheDocument();
  });

  it("runs immediately on click when the action takes no input", async () => {
    runMock.mockResolvedValue(outcome("queued"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() =>
      expect(runMock).toHaveBeenCalledWith({ quickActionId: "qa-1" }),
    );
    await waitFor(() => expect(toastCalls[0]?.kind).toBe("success"));
    expect(toastCalls[0]?.message).toContain("Lambda");
  });

  it("reports a coalesced run as queued-behind, never as a fresh start", async () => {
    runMock.mockResolvedValue(outcome("coalesced"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() => expect(toastCalls).toHaveLength(1));
    expect(toastCalls[0]?.kind).toBe("info");
    expect(toastCalls[0]?.message.toLowerCase()).toContain("added to");
  });

  it("reports an offline target as deferred rather than started", async () => {
    runMock.mockResolvedValue(outcome("deferred"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() => expect(toastCalls).toHaveLength(1));
    expect(toastCalls[0]?.kind).toBe("info");
    expect(toastCalls[0]?.message.toLowerCase()).toContain("offline");
  });

  it("does not claim success for an unknown server status", async () => {
    runMock.mockResolvedValue(outcome("some_future_status"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() => expect(toastCalls).toHaveLength(1));
    expect(toastCalls[0]?.kind).not.toBe("success");
  });

  it("still offers an action the viewer cannot run, and explains the refusal in a dialog", async () => {
    // The list is intentionally unfiltered, so this path is reachable by
    // design — the explanation is the whole point of not hiding the button.
    reasonCode = "invocation_not_allowed";
    runMock.mockRejectedValue(new Error("forbidden"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() => expect(screen.getByRole("alertdialog")).toBeInTheDocument());
    expect(screen.getByText(/can’t run this quick action/i)).toBeInTheDocument();
    // A refusal is explained, not toasted as a transient failure.
    expect(toastCalls).toHaveLength(0);
  });

  it("toasts a non-permission failure instead of opening the dialog", async () => {
    reasonCode = undefined;
    runMock.mockRejectedValue(new Error("network down"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    await waitFor(() => expect(toastCalls).toHaveLength(1));
    expect(toastCalls[0]?.kind).toBe("error");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("runs on a single click — there is no per-action input step", () => {
    // The `/` slash command covers "same action, one detail different" by
    // dropping the rendered body into the composer, so the sidebar row stays a
    // plain button with no second interaction tier.
    runMock.mockResolvedValue(outcome("queued"));
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    fireEvent.click(screen.getByText("Code Review"));

    expect(runMock).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("shows more actions from a text row below the list instead of a header icon", () => {
    listData = [
      action({ id: "a", name: "One" }),
      action({ id: "b", name: "Two" }),
      action({ id: "c", name: "Three" }),
      action({ id: "d", name: "Four" }),
      action({ id: "e", name: "Five" }),
      action({ id: "f", name: "Six" }),
      action({ id: "archived", name: "Archived", status: "archived" }),
    ];
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    expect(screen.getByRole("button", { name: /Quick actions 6/i })).toBeVisible();
    expect(screen.queryByText("Archived")).not.toBeInTheDocument();
    expect(screen.getByText("Five")).toBeInTheDocument();
    expect(screen.queryByText("Six")).not.toBeInTheDocument();

    const disclosure = screen.getByRole("button", { name: /Quick actions 6/i });
    const more = screen.getByRole("button", { name: "Show 1 more" });
    expect(more).toHaveTextContent("Show 1 more");
    expect(within(disclosure.parentElement!).queryByRole("button", { name: "Show 1 more" })).not.toBeInTheDocument();
    expect(more.compareDocumentPosition(screen.getByText("Five")) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    fireEvent.click(more);
    expect(screen.getByText("Six")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show 1 more" })).not.toBeInTheDocument();
    fireEvent.click(disclosure);
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("button", { name: /Quick actions 6/i })).toBeVisible();
  });
});
