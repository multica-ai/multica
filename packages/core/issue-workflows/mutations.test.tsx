// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import type { IssueWorkflowResponse } from "../types";
import { useApplyProjectWorkflow } from "./mutations";
import { effectiveIssueWorkflowOptions, issueWorkflowOptions } from "./queries";

const apply = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { applyProjectWorkflow: apply } }));
vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws" }));

it("refreshes project status choices without leaking archived statuses or replacing another pinned workflow", async () => {
  const client = new QueryClient();
  const saved = {
    workflow: { id: "new-flow", revision: 2 },
    mode: "custom",
    statuses: [
      { id: "ready", archived_at: null },
      { id: "retired", archived_at: "2026-09-07" },
    ],
  } as IssueWorkflowResponse;
  const previous = { ...saved, workflow: { ...saved.workflow, id: "old-flow" } };
  const choices = effectiveIssueWorkflowOptions("ws", "project").queryKey;
  const editor = effectiveIssueWorkflowOptions("ws", "project", true).queryKey;
  const otherProject = effectiveIssueWorkflowOptions("ws", "other").queryKey;
  const otherWorkspace = effectiveIssueWorkflowOptions("other-ws", "project").queryKey;
  const pinned = issueWorkflowOptions("ws", "old-flow").queryKey;
  for (const key of [choices, editor, otherProject, otherWorkspace, pinned]) {
    client.setQueryData(key, previous);
  }
  apply.mockResolvedValue(saved);
  const { result, unmount } = renderHook(() => useApplyProjectWorkflow(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
  await act(async () => {
    await result.current.mutateAsync({
      projectId: "project",
      data: {
        mode: "custom",
        expected_revision: 1,
        allow_archive: true,
        spec: { api_version: 1, name: "Workflow", initial_status: "ready", statuses: [] },
      },
    });
  });
  expect(client.getQueryData(choices)).toEqual({ ...saved, statuses: [saved.statuses[0]] });
  expect(client.getQueryData(editor)).toEqual(saved);
  for (const key of [otherProject, otherWorkspace, pinned]) {
    expect(client.getQueryData(key)).toEqual(previous);
  }
  unmount();
  client.clear();
});

it("does not replace live workflow choices with rolled-back preview node IDs", async () => {
  const client = new QueryClient();
  const choices = effectiveIssueWorkflowOptions("ws", "project").queryKey;
  const existing = { workflow: { id: "live", workspace_id: "ws", scope_type: "workspace", scope_id: "ws", name: "Default", revision: 1, initial_status_id: null, created_at: "", updated_at: "" }, statuses: [], mode: "default" };
  client.setQueryData(choices, existing);
  apply.mockResolvedValue({ workflow: { id: "transient" }, statuses: [], mode: "custom", dry_run: true });
  const { result, unmount } = renderHook(() => useApplyProjectWorkflow(), {
    wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
  });
  await act(async () => { await result.current.mutateAsync({ projectId: "project", data: { mode: "custom", expected_revision: 1, dry_run: true } }); });
  expect(client.getQueryData(choices)).toEqual(existing);
  expect(client.getQueryState(choices)?.isInvalidated).toBe(false);
  unmount(); client.clear();
});
