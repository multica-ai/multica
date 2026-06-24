import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { RunConfirmModal } from "./run-confirm";

// --- Controllable preview result ---------------------------------------------
const previewState = {
  triggers: [{ issue_id: "issue-1", agent_id: "agent-1", source: "assign", handoff_supported: true }],
  totalCount: 1,
  isLoading: false,
  handoffSupported: true,
};
vi.mock("../issues/hooks/use-issue-trigger-preview", () => ({
  useIssueTriggerPreview: () => previewState,
}));

const mockUpdate = vi.fn().mockResolvedValue(undefined);
const mockBatch = vi.fn().mockResolvedValue(undefined);
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutateAsync: mockUpdate }),
  useBatchUpdateIssues: () => ({ mutateAsync: mockBatch }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Walt" }),
}));

vi.mock("../i18n", () => ({
  useT: () => ({ t: (sel: (x: Record<string, Record<string, string>>) => string) => {
    // Resolve the accessor against a flat label map so assertions can target text.
    const labels = {
      run_confirm: {
        title_assign: "Assign and start?",
        title_status: "Start working now?",
        will_start_named: "start Walt",
        will_start: "start many",
        nothing_assign: "no run (backlog)",
        nothing_status: "no runs",
        note_label: "Handoff note",
        note_placeholder: "scope...",
        note_unsupported: "runtime too old",
        start: "Start",
        dont_start: "Don't start yet",
        apply: "Apply",
        toast_failed: "failed",
        create_will_start: "create start",
        create_parked: "create parked",
      },
    };
    return sel(labels);
  } }),
}));

// Keep the ui primitives as light DOM so the logic is what's under test.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
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
vi.mock("@multica/ui/components/ui/textarea", () => ({
  Textarea: (props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

beforeEach(() => {
  mockUpdate.mockClear();
  mockBatch.mockClear();
  previewState.triggers = [{ issue_id: "issue-1", agent_id: "agent-1", source: "assign", handoff_supported: true }];
  previewState.totalCount = 1;
  previewState.handoffSupported = true;
});

describe("RunConfirmModal", () => {
  it("single assign + Start sends the assignee change with the handoff note", async () => {
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }}
      />,
    );
    fireEvent.change(screen.getByPlaceholderText("scope..."), { target: { value: "only login" } });
    fireEvent.click(screen.getByText("Start"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      assignee_type: "agent",
      assignee_id: "agent-1",
      handoff_note: "only login",
    });
    expect(mockBatch).not.toHaveBeenCalled();
  });

  it("'暂不开始' sends suppress_run and no handoff note", async () => {
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }}
      />,
    );
    fireEvent.change(screen.getByPlaceholderText("scope..."), { target: { value: "ignored" } });
    fireEvent.click(screen.getByText("Don't start yet"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    const payload = mockUpdate.mock.calls[0]![0];
    expect(payload.suppress_run).toBe(true);
    expect(payload.handoff_note).toBeUndefined();
  });

  it("disables the note box when the runtime can't render a handoff", () => {
    previewState.handoffSupported = false;
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }}
      />,
    );
    expect(screen.getByPlaceholderText("scope...")).toBeDisabled();
    expect(screen.getByText("runtime too old")).toBeInTheDocument();
  });

  it("batch (N ids) applies via batchUpdate", async () => {
    previewState.triggers = [
      { issue_id: "i1", agent_id: "a1", source: "status", handoff_supported: true },
      { issue_id: "i2", agent_id: "a2", source: "status", handoff_supported: true },
    ];
    previewState.totalCount = 2;
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["i1", "i2"], mode: "status", status: "todo" }}
      />,
    );
    fireEvent.click(screen.getByText("Start"));
    await waitFor(() => expect(mockBatch).toHaveBeenCalledTimes(1));
    expect(mockBatch).toHaveBeenCalledWith({ ids: ["i1", "i2"], updates: { status: "todo" } });
    expect(mockUpdate).not.toHaveBeenCalled();
  });
});
