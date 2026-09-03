import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import {
  configureShortcutPlatform,
  createShortcutChord,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { RunConfirmModal } from "./run-confirm";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () =>
    buildIssueStatusCatalog([
      {
        id: "rework",
        workspace_id: "ws-test",
        key: "rework",
        name: "Rework",
        description: "",
        category: "todo",
        color: "#22c55e",
        is_system: false,
        position: 0,
        archived_at: null,
        created_at: "",
        updated_at: "",
      },
    ]),
}));

const mockUpdate = vi.fn().mockResolvedValue({ id: "issue-1" });
const mockBatch = vi.fn().mockResolvedValue({ updated: 2 });
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: mockUpdate }),
  useBatchUpdateIssues: () => ({ mutateAsync: mockBatch }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Walt" }),
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      sel: (x: Record<string, Record<string, string>>) => string,
      vars?: Record<string, unknown>,
    ) => {
      // Resolve the accessor against a flat label map so assertions can target
      // text, then interpolate {{name}} / {{count}} the way i18next would — the
      // headline substitutes the assignee name and the batch count.
      const labels = {
        run_confirm: {
          title_assign: "Confirm assignment?",
          assign_single: "assign to {{name}}",
          assign_batch: "assign {{count}} to {{name}}",
          confirm_assign: "Confirm assignment",
          dont_start: "Don't start yet",
          toast_failed: "failed",
          title_promote: "Start work now?",
          promote_single: "move to {{status}}, {{name}} starts",
          confirm_promote: "Move and start",
        },
        // useStatusLabel resolves BUILT-IN keys through i18n and custom ones
        // through the catalog, so the promote headline needs both sources.
        status: { todo: "Todo" },
      };
      return sel(labels).replace(/\{\{(\w+)\}\}/g, (_m, k) => String(vars?.[k] ?? ""));
    },
  }),
}));

// Keep the ui primitives as light DOM so the logic is what's under test.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  // Keeps the real Popup's prop passthrough, which the send chord binds to.
  DialogContent: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
    <div data-testid="dialog-content" {...props}>{children}</div>
  ),
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));
vi.mock("@multica/ui/components/ui/spinner", () => ({
  Spinner: () => <span data-testid="spinner" />,
}));
// vi.hoisted: vi.mock factories run before module-level consts initialize.
// Only error is used now — completion is silent (no result toast).
const mockToast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));
vi.mock("sonner", () => ({ toast: mockToast }));

beforeEach(() => {
  mockUpdate.mockClear().mockResolvedValue({ id: "issue-1" });
  mockBatch.mockClear().mockResolvedValue({ updated: 2 });
  mockToast.error.mockClear();
  mockToast.success.mockClear();
  // The real shortcut store drives both the submit chord and the keycap hint,
  // and jsdom's platform follows the host OS — pin it so the chord is ⌘+Enter
  // everywhere, not Ctrl+Enter on a Linux CI runner.
  configureShortcutPlatform("macos");
  useShortcutStore.setState({ overrides: {} });
});

afterEach(() => {
  configureShortcutPlatform(null);
  useShortcutStore.setState({ overrides: {} });
});

const confirmButton = () => screen.getByRole("button", { name: "Confirm assignment" });
const dialog = () => screen.getByTestId("dialog-content");

const single = {
  issueIds: ["issue-1"],
  mode: "assign" as const,
  assigneeType: "agent" as const,
  assigneeId: "agent-1",
};

// Promoting a parked issue out of backlog starts the run on its own, so it
// confirms through this same dialog — one behaviour for built-in `todo` and
// every custom Todo-category status alike (MUL-6463).
const promote = {
  issueIds: ["issue-1"],
  mode: "promote" as const,
  status: "rework",
  assigneeType: "agent" as const,
  assigneeId: "agent-1",
};

describe("RunConfirmModal", () => {
  it("is fully operable on the first frame — no preview request, no spinner", () => {
    // The MUL-5010 core: opening the dialog fires nothing and blocks nothing.
    const { container } = render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
    expect(confirmButton()).not.toBeDisabled();
    // Headline reads across elements — the assignee name is bolded in place.
    expect(container.textContent).toContain("assign to Walt");
  });

  it("single assign sends the assignee change and nothing else", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.click(confirmButton());
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      assignee_type: "agent",
      assignee_id: "agent-1",
    });
    expect(mockBatch).not.toHaveBeenCalled();
  });

  it("completes silently on success — closes with no result toast", async () => {
    // Final scope: the dialog only confirms the assignment. The assignee and any
    // run surface through the issue's normal updates, so submit adds no toast.
    const onClose = vi.fn();
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(confirmButton());
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(mockToast.error).not.toHaveBeenCalled();
  });

  it("'暂不开始' sends suppress_run alongside the assignee change", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.click(screen.getByText("Don't start yet"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      assignee_type: "agent",
      assignee_id: "agent-1",
      suppress_run: true,
    });
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("promote sends the status change with no assignee fields", async () => {
    // The owner is already on the issue: re-sending it would turn a status
    // write into an assignee write on the server's side of the predicate.
    render(<RunConfirmModal onClose={vi.fn()} data={promote} />);
    fireEvent.click(screen.getByRole("button", { name: "Move and start" }));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      status: "rework",
    });
  });

  it("promote's 'don't start yet' still moves the issue, without the run", async () => {
    // The status change is the point; suppress_run is the only difference. This
    // is the one way to leave backlog WITHOUT waking the agent.
    render(<RunConfirmModal onClose={vi.fn()} data={promote} />);
    fireEvent.click(screen.getByText("Don't start yet"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      status: "rework",
      suppress_run: true,
    });
  });

  it("promote names the target status the way the workspace named it", () => {
    // A custom status is only recognisable by its catalog name; built-ins keep
    // resolving through i18n so a zh workspace never reads "In Progress".
    const { container, rerender } = render(<RunConfirmModal onClose={vi.fn()} data={promote} />);
    expect(screen.getByText("Start work now?")).toBeInTheDocument();
    expect(container.textContent).toContain("move to Rework, Walt starts");

    rerender(<RunConfirmModal onClose={vi.fn()} data={{ ...promote, status: "todo" }} />);
    expect(container.textContent).toContain("move to Todo, Walt starts");
  });

  it("batch assign (N ids) applies via batchUpdate", async () => {
    const { container } = render(
      <RunConfirmModal onClose={vi.fn()} data={{ ...single, issueIds: ["i1", "i2"] }} />,
    );
    expect(container.textContent).toContain("assign 2 to Walt");
    fireEvent.click(confirmButton());
    await waitFor(() => expect(mockBatch).toHaveBeenCalledTimes(1));
    expect(mockBatch).toHaveBeenCalledWith({
      ids: ["i1", "i2"],
      updates: { assignee_type: "agent", assignee_id: "agent-1" },
    });
    expect(mockUpdate).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  // --- Send chord (MUL-5694) ------------------------------------------------
  // The chord is bound on the dialog, not on a single control, so it confirms
  // wherever focus happens to be.

  it("confirms on the send chord typed anywhere in the dialog", async () => {
    const onClose = vi.fn();
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.keyDown(dialog(), { key: "Enter", metaKey: true });
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      assignee_type: "agent",
      assignee_id: "agent-1",
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("confirms from a focused footer button, which the chord cannot activate", async () => {
    // Chromium fires no click for ⌘/Ctrl+Enter on a focused button, so without
    // the dialog handling it there the chord is simply dead. The dialog focuses
    // its first tabbable child, which is now a footer button.
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.keyDown(screen.getByText("Don't start yet"), { key: "Enter", metaKey: true });
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    // The primary action, not the button the caret happened to sit on.
    expect(mockUpdate.mock.calls[0]![0].suppress_run).toBeUndefined();
  });

  it("yields to a focused button when send is remapped to plain Enter", () => {
    // A bare Enter DOES activate a focused button, so confirming here as well
    // would double-write — and on "Don't start yet" the two would disagree.
    useShortcutStore.setState({ overrides: { send: createShortcutChord("Enter") } });
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.keyDown(screen.getByText("Don't start yet"), { key: "Enter" });
    fireEvent.keyDown(confirmButton(), { key: "Enter" });
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("submits once for a held chord, and never for an IME's committing Enter", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.keyDown(dialog(), { key: "Enter", metaKey: true, isComposing: true });
    fireEvent.keyDown(dialog(), { key: "Enter", metaKey: true, repeat: true });
    expect(mockUpdate).not.toHaveBeenCalled();
    fireEvent.keyDown(dialog(), { key: "Enter", metaKey: true });
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
  });

  it("follows a remapped send chord instead of hardcoding ⌘+Enter", async () => {
    useShortcutStore.setState({ overrides: { send: createShortcutChord("Enter") } });
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.keyDown(dialog(), { key: "Enter", metaKey: true });
    expect(mockUpdate).not.toHaveBeenCalled();
    fireEvent.keyDown(dialog(), { key: "Enter" });
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
  });

  it("shows the chord on the confirm button without renaming it", () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    // Decorative: discoverable next to the label, absent from the a11y name —
    // `confirmButton()` resolving by that exact name is the assertion.
    expect(
      confirmButton().querySelector('[data-slot="shortcut-keycaps"]'),
    ).toBeInTheDocument();
  });

  it("keeps the dialog open and surfaces the error when the write fails", async () => {
    const onClose = vi.fn();
    mockUpdate.mockRejectedValue(new Error("boom"));
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(confirmButton());
    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith("boom"));
    expect(onClose).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });
});
