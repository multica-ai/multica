import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
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

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: listData, isLoading: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1" }),
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
    toastCalls.length = 0;
    reasonCode = undefined;
    listData = [action()];
  });

  it("renders nothing when the workspace has no actions", () => {
    listData = [];
    const { container } = renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("hides archived actions even if the server returned them", () => {
    listData = [action({ status: "archived" })];
    const { container } = renderWithI18n(<QuickActionsSection issueId="i-1" />);
    expect(container).toBeEmptyDOMElement();
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

  it("collapses everything past the sidebar limit behind More", () => {
    listData = [
      action({ id: "a", name: "One" }),
      action({ id: "b", name: "Two" }),
      action({ id: "c", name: "Three" }),
      action({ id: "d", name: "Four" }),
      action({ id: "e", name: "Five" }),
      action({ id: "f", name: "Six" }),
    ];
    renderWithI18n(<QuickActionsSection issueId="i-1" />);

    expect(screen.getByText("Five")).toBeInTheDocument();
    expect(screen.queryByText("Six")).not.toBeInTheDocument();

    fireEvent.click(screen.getByText(/Show 1 more/i));
    expect(screen.getByText("Six")).toBeInTheDocument();
  });
});
