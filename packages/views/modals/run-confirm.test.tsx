import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { RunConfirmModal } from "./run-confirm";

// --- Warm agent / squad / runtime caches (prefetched in the real app) --------
// The modal resolves the target runtime's cli_version locally — an agent's own
// runtime, or a squad leader's — so nothing in the dialog waits on the network.
// Tests drive the verdict by swapping the runtime's reported cli_version here.
const cache = {
  agents: [{ id: "agent-1", runtime_id: "runtime-1" }] as Array<{ id: string; runtime_id: string }>,
  runtimes: [{ id: "runtime-1", metadata: { cli_version: "0.4.0" } }] as Array<{
    id: string;
    metadata: Record<string, unknown>;
  }>,
  squads: [{ id: "squad-1", leader_id: "agent-1" }] as Array<{ id: string; leader_id: string }>,
};
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "runtimes") return { data: cache.runtimes };
    if (queryKey[0] === "workspaces" && queryKey[2] === "agents") return { data: cache.agents };
    if (queryKey[0] === "workspaces" && queryKey[2] === "squads") return { data: cache.squads };
    return { data: [] };
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
  squadListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "squads"] }),
}));
// Stub the runtimes barrel: the query-options builder would otherwise drag the
// network layer in, and the deep cli-version module isn't an exported subpath.
// `handoffSupported`'s real semver/dev-build logic is exhaustively covered in
// packages/core/runtimes/cli-version.test.ts; here we only need a faithful
// stand-in for the >= 0.3.28 threshold so the cache → version → verdict wiring
// is exercised end to end.
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: (wsId: string) => ({ queryKey: ["runtimes", wsId, "list"] }),
  readRuntimeCliVersion: (m?: { cli_version?: unknown }) =>
    typeof m?.cli_version === "string" ? m.cli_version : "",
  handoffSupported: (v?: string | null) => {
    const m = /(\d+)\.(\d+)\.(\d+)/.exec((v ?? "").trim());
    if (!m) return false;
    return Number(m[1]) * 1e6 + Number(m[2]) * 1e3 + Number(m[3]) >= 3028; // 0.3.28
  },
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
          note_label: "Handoff note",
          note_placeholder: "scope...",
          note_unsupported: "runtime too old",
          confirm_assign: "Confirm assignment",
          dont_start: "Don't start yet",
          toast_failed: "failed",
        },
      };
      return sel(labels).replace(/\{\{(\w+)\}\}/g, (_m, k) => String(vars?.[k] ?? ""));
    },
  }),
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
  cache.agents = [{ id: "agent-1", runtime_id: "runtime-1" }];
  cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.4.0" } }];
  cache.squads = [{ id: "squad-1", leader_id: "agent-1" }];
});

const single = {
  issueIds: ["issue-1"],
  mode: "assign" as const,
  assigneeType: "agent" as const,
  assigneeId: "agent-1",
};

describe("RunConfirmModal", () => {
  it("is fully operable on the first frame — no preview request, no spinner", () => {
    // The MUL-5010 core: opening the dialog fires nothing and blocks nothing.
    const { container } = render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("scope...")).not.toBeDisabled();
    expect(screen.getByText("Confirm assignment")).not.toBeDisabled();
    // Headline reads across elements — the assignee name is bolded in place.
    expect(container.textContent).toContain("assign to Walt");
  });

  it("single assign sends the assignee change with the handoff note", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.change(screen.getByPlaceholderText("scope..."), { target: { value: "only login" } });
    fireEvent.click(screen.getByText("Confirm assignment"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: "issue-1",
      assignee_type: "agent",
      assignee_id: "agent-1",
      handoff_note: "only login",
    });
    expect(mockBatch).not.toHaveBeenCalled();
  });

  it("completes silently on success — closes with no result toast", async () => {
    // Final scope: the dialog only confirms the assignment. The assignee and any
    // run surface through the issue's normal updates, so submit adds no toast.
    const onClose = vi.fn();
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(screen.getByText("Confirm assignment"));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(mockToast.error).not.toHaveBeenCalled();
  });

  it("'暂不开始' sends suppress_run and no handoff note", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.change(screen.getByPlaceholderText("scope..."), { target: { value: "ignored" } });
    fireEvent.click(screen.getByText("Don't start yet"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    const payload = mockUpdate.mock.calls[0]![0];
    expect(payload.suppress_run).toBe(true);
    expect(payload.handoff_note).toBeUndefined();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("disables the note box when the agent's runtime is too old", () => {
    cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.2.21" } }];
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.getByPlaceholderText("scope...")).toBeDisabled();
    expect(screen.getByText("runtime too old")).toBeInTheDocument();
  });

  it("resolves a squad's verdict through its leader's runtime, locally", () => {
    // A squad run is executed by its leader, so the leader's runtime decides.
    // The squad list gives us leader_id, so this needs no server verdict.
    cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.2.21" } }];
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ ...single, assigneeType: "squad", assigneeId: "squad-1" }}
      />,
    );
    expect(screen.getByPlaceholderText("scope...")).toBeDisabled();
    expect(screen.getByText("runtime too old")).toBeInTheDocument();
  });

  it("leaves the note box enabled when the target runtime can't be resolved", () => {
    // Unknown assignee → no verdict. The note is a soft gate, so an
    // unresolvable target must not produce a spurious warning.
    cache.agents = [];
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.getByPlaceholderText("scope...")).not.toBeDisabled();
    expect(screen.queryByText("runtime too old")).not.toBeInTheDocument();
  });

  it("batch assign (N ids) applies via batchUpdate", async () => {
    const { container } = render(
      <RunConfirmModal onClose={vi.fn()} data={{ ...single, issueIds: ["i1", "i2"] }} />,
    );
    expect(container.textContent).toContain("assign 2 to Walt");
    fireEvent.click(screen.getByText("Confirm assignment"));
    await waitFor(() => expect(mockBatch).toHaveBeenCalledTimes(1));
    expect(mockBatch).toHaveBeenCalledWith({
      ids: ["i1", "i2"],
      updates: { assignee_type: "agent", assignee_id: "agent-1" },
    });
    expect(mockUpdate).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("keeps the dialog open and surfaces the error when the write fails", async () => {
    const onClose = vi.fn();
    mockUpdate.mockRejectedValue(new Error("boom"));
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(screen.getByText("Confirm assignment"));
    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith("boom"));
    expect(onClose).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });
});
