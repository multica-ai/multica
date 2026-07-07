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

// --- Warm agent + runtime caches (prefetched in the real app) ----------------
// The modal resolves a concrete agent assignee → its runtime → cli_version
// locally, exactly like the quick-create version gate, so the note box never
// waits on the preview round-trip. Tests drive the local verdict by swapping
// the runtime's reported cli_version here.
const cache = {
  agents: [{ id: "agent-1", runtime_id: "runtime-1" }] as Array<{ id: string; runtime_id: string }>,
  runtimes: [{ id: "runtime-1", metadata: { cli_version: "0.4.0" } }] as Array<{
    id: string;
    metadata: Record<string, unknown>;
  }>,
};
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "runtimes") return { data: cache.runtimes };
    if (queryKey[0] === "workspaces" && queryKey[2] === "agents") return { data: cache.agents };
    return { data: [] };
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
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
        will_start_named_squad: "start squad Walt",
        will_start: "start many",
        will_start_squad: "start squad many",
        nothing_assign: "no run (backlog)",
        nothing_status: "no runs",
        checking: "Checking…",
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
vi.mock("@multica/ui/components/ui/spinner", () => ({
  Spinner: () => <span data-testid="spinner" />,
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

beforeEach(() => {
  mockUpdate.mockClear();
  mockBatch.mockClear();
  previewState.triggers = [{ issue_id: "issue-1", agent_id: "agent-1", source: "assign", handoff_supported: true }];
  previewState.totalCount = 1;
  previewState.isLoading = false;
  previewState.handoffSupported = true;
  cache.agents = [{ id: "agent-1", runtime_id: "runtime-1" }];
  cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.4.0" } }];
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

  it("disables the note box from the local runtime version, before the preview resolves", () => {
    // Old daemon that can't render handoff notes, and the predicate is still in
    // flight. The box must already be disabled + warned from the warm runtime
    // cache — no "checking…" wait, no reliance on the server verdict.
    previewState.isLoading = true;
    previewState.totalCount = 0;
    cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.2.21" } }];
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }}
      />,
    );
    expect(screen.getByPlaceholderText("scope...")).toBeDisabled();
    expect(screen.getByText("runtime too old")).toBeInTheDocument();
  });

  it("keeps the note box usable while the preview is still loading for a supported agent", () => {
    // The core of MUL-3706: a concrete agent on a current runtime should never
    // see a "checking…" gate on the note box — the version is known locally.
    previewState.isLoading = true;
    previewState.totalCount = 0;
    cache.runtimes = [{ id: "runtime-1", metadata: { cli_version: "0.4.0" } }];
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "agent", assigneeId: "agent-1" }}
      />,
    );
    expect(screen.getByPlaceholderText("scope...")).not.toBeDisabled();
    expect(screen.queryByText("runtime too old")).not.toBeInTheDocument();
  });

  it("squad assignee defers to the server handoff verdict (not locally resolvable)", () => {
    // A squad routes to its leader agent, picked server-side — the target
    // runtime isn't knowable client-side, so the box must follow the preview's
    // handoff_supported, exactly as before.
    previewState.handoffSupported = false;
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ issueIds: ["issue-1"], mode: "assign", assigneeType: "squad", assigneeId: "squad-1" }}
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
