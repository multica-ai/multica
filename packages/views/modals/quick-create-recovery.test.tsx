import { useState, type ReactNode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";
import { setCurrentWorkspace, defaultStorage } from "@multica/core/platform";
import { CreateIssueDialog } from "./create-issue-dialog";

const state = vi.hoisted(() => ({ workspaceId: "ws-recovery", projectFetching: false }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => state.workspaceId }));
vi.mock("@tanstack/react-query", () => ({ queryOptions: (options: unknown) => options, useQuery: () => ({
  data: [{ id: "project-a", archived_at: null }, { id: "archived-a", archived_at: "2026-01-01T00:00:00Z" }], isSuccess: true, isFetching: state.projectFetching, isError: false, refetch: vi.fn(),
}) }));
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open, onOpenChangeComplete }: {
    children: ReactNode;
    open?: boolean;
    onOpenChangeComplete?: (open: boolean) => void;
  }) => <div>
    {children}
    {open === false && <button onClick={() => onOpenChangeComplete?.(false)}>finish exit</button>}
  </div>,
  DialogContent: ({ children }: {children: ReactNode}) => <div>{children}</div>,
}));
vi.mock("./quick-create-issue", () => ({ AgentCreatePanel: () => <div>agent panel</div> }));
vi.mock("./create-issue", () => ({
  manualDialogContentClass: () => "",
  ManualCreatePanel: ({ onClose, recoveryProjects }: {onClose: () => void; recoveryProjects?: { activeIds: ReadonlySet<string> | null }}) => {
    const draft = useIssueDraftStore((s) => s.draft);
    const [title, setTitle] = useState(draft.manual.title);
    return <div>
      <output data-testid="panel-title">{title}</output>
      <output data-testid="project-catalog-ready">{String(recoveryProjects?.activeIds !== null)}</output>
      <output data-testid="active-projects">{JSON.stringify(Array.from(recoveryProjects?.activeIds ?? []))}</output>
      <button onClick={() => { setTitle("Edited recovery A"); useIssueDraftStore.getState().setManual({ title: "Edited recovery A" }); }}>edit recovery title</button>
      <output data-testid="recovery-draft">{JSON.stringify(draft)}</output>
      <button onClick={onClose}>cancel recovery</button>
      <button onClick={() => { useIssueDraftStore.getState().clearDraft(); onClose(); }}>accept recovery</button>
    </div>;
  },
}));

const recovery = { notificationId: "failure-a", workspaceId: "ws-recovery", projectId: "project-a", description: "Original request A", agentId: "agent-a" };
function Host({ seed = recovery }: {seed?: typeof recovery}) {
  const [open, setOpen] = useState(true);
  return open ? <CreateIssueDialog initialMode="manual" onClose={() => setOpen(false)} data={{ quick_create_recovery: seed }} /> : null;
}
function RetainedHost() {
  const [open, setOpen] = useState(true);
  const [mounted, setMounted] = useState(true);
  return mounted ? (
    <CreateIssueDialog
      open={open}
      initialMode="manual"
      onClose={() => setOpen(false)}
      onOpenChangeComplete={(nextOpen) => { if (!nextOpen) setMounted(false); }}
      data={{ quick_create_recovery: recovery }}
    />
  ) : <output data-testid="released-recovery" />;
}
const flush = () => new Promise<void>((resolve) => queueMicrotask(resolve));
beforeEach(async () => {
  state.workspaceId = "ws-recovery";
  state.projectFetching = false;
  setCurrentWorkspace("recovery-test", state.workspaceId);
  await flush();
  useIssueDraftStore.setState({ isolatedDraftBackup: undefined });
  useIssueDraftStore.getState().clearDraft();
  useIssueDraftStore.getState().setManual({ title: "Unfinished B", description: "Body B" });
  useIssueDraftStore.getState().setShared({ projectId: "project-b", attachments: [{ clientUploadId: "upload-b", filename: "B.txt", size: 1, status: "failed", error: "synthetic" }] });
});
afterEach(async () => {
  useIssueDraftStore.setState({ isolatedDraftBackup: undefined });
  setCurrentWorkspace(null, null); await flush();
  defaultStorage.removeItem("multica_issue_draft:recovery-test");
  defaultStorage.removeItem("multica_issue_draft:recovery-other-test");
});

describe("quick-create recovery isolation", () => {
  it.each(["cancel recovery", "accept recovery"])("preserves the real ordinary draft through %s", (action) => {
    const ordinary = useIssueDraftStore.getState().draft;
    render(<Host />);
    const draft = useIssueDraftStore.getState().draft;
    expect(draft.manual.title).toBe("");
    expect(draft.manual.description).toBe(recovery.description);
    expect(draft.shared.projectId).toBe("project-a");
    expect(draft.shared.attachments).toEqual([]);
    const persisted = defaultStorage.getItem("multica_issue_draft:recovery-test");
    expect(persisted).toContain("Unfinished B");
    expect(persisted).not.toContain("Original request A");
    fireEvent.click(screen.getByRole("button", {name: action}));
    expect(useIssueDraftStore.getState().draft).toEqual(ordinary);
  });

  it("keeps the isolated draft until the dialog exit completes", () => {
    const ordinary = useIssueDraftStore.getState().draft;
    render(<RetainedHost />);
    fireEvent.click(screen.getByRole("button", { name: "cancel recovery" }));

    expect(screen.getByTestId("recovery-draft")).toHaveTextContent("Original request A");
    expect(useIssueDraftStore.getState().isolatedDraftBackup).toEqual(ordinary);
    fireEvent.click(screen.getByRole("button", { name: "finish exit" }));

    expect(screen.getByTestId("released-recovery")).toBeInTheDocument();
    expect(useIssueDraftStore.getState().draft).toEqual(ordinary);
  });

  it("releases the retained dialog when workspace changes remove its draft", async () => {
    const view = render(<RetainedHost />);
    await act(async () => {
      state.workspaceId = "ws-other";
      setCurrentWorkspace("recovery-other-test", state.workspaceId);
      await flush();
      useIssueDraftStore.getState().setManual({ title: "Other workspace C" });
    });
    view.rerender(<RetainedHost />);

    expect(screen.getByTestId("released-recovery")).toBeInTheDocument();
    expect(useIssueDraftStore.getState().draft.manual.title).toBe("Other workspace C");
  });

  it("does not classify a cached catalog while its refetch is pending", () => {
    state.projectFetching = true;
    render(<Host />);
    expect(screen.getByTestId("project-catalog-ready")).toHaveTextContent("false");
    expect(useIssueDraftStore.getState().draft.shared.projectId).toBe("project-a");
  });

  it("filters archived projects from the recovery catalog", () => {
    render(<Host />);
    expect(JSON.parse(screen.getByTestId("active-projects").textContent ?? "[]")).toEqual(["project-a"]);
  });

  it("mounts a fresh form when another failed notification replaces the recovery", () => {
    const view = render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "edit recovery title" }));
    expect(screen.getByTestId("panel-title")).toHaveTextContent("Edited recovery A");
    view.rerender(<Host seed={{ ...recovery, notificationId: "failure-b", description: "Different failed request" }} />);
    expect(screen.getByTestId("panel-title")).toHaveTextContent(/^$/);
    expect(useIssueDraftStore.getState().draft.manual.description).toBe("Different failed request");
  });

  it("does not restore the old backup after a workspace switch", async () => {
    const view = render(<Host />);
    await act(async () => {
      state.workspaceId = "ws-other";
      setCurrentWorkspace("recovery-other-test", state.workspaceId);
      await flush();
      useIssueDraftStore.getState().setManual({ title: "Other workspace C" });
    });
    view.rerender(<Host />);
    expect(screen.queryByTestId("recovery-draft")).not.toBeInTheDocument();
    view.unmount();
    expect(useIssueDraftStore.getState().draft.manual.title).toBe("Other workspace C");
    expect(defaultStorage.getItem("multica_issue_draft:recovery-other-test")).not.toContain("Unfinished B");
  });
});
