// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ProjectResource } from "@multica/core/types";
import { projectResourceKeys } from "@multica/core/projects";
import { renderWithI18n } from "../../test/i18n";

const { listResources } = vi.hoisted(() => ({ listResources: vi.fn() }));
vi.mock("@multica/core/api", () => ({ api: { listProjectResources: listResources } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ repos: [] }) }));
vi.mock("@multica/core/config", () => ({ useConfigStore: (select: (state: { localWorktreeSupported: boolean }) => unknown) => select({ localWorktreeSupported: true }) }));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: async () => [] }),
  runtimeAdvertisesLocalWorktree: () => true,
}));
vi.mock("../../platform/local-directory", () => ({ isDesktopShell: () => false, pickDirectory: vi.fn(), validateLocalDirectory: vi.fn() }));
vi.mock("../../platform/use-local-daemon-status", () => ({ useLocalDaemonStatus: () => ({ daemonId: null, running: false }) }));

import { ProjectResourcesSection } from "./project-resources-section";
const resource: ProjectResource = {
  id: "r", project_id: "p", workspace_id: "ws", resource_type: "local_directory",
  resource_ref: { local_path: "/repo", daemon_id: "d", execution_mode: "worktree" },
  position: 0, label: "Repository", created_at: "2026-10-06T00:00:00Z", created_by: null,
  worktree_readiness: {
    status: "blocked", checked_at: "2026-10-06T00:00:00Z", expires_at: "2099-10-06T00:00:00Z",
    file_count: 234, total_bytes: 1258291200, max_files: 2000, max_bytes: 209715200,
    largest_paths: [{ path: "debug", file_count: 234, total_bytes: 1258291200 }],
  },
};
const clients: QueryClient[] = [];
function setup(value = resource) {
  listResources.mockResolvedValue({ resources: [value], total: 1 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><ProjectResourcesSection projectId="p" /></QueryClientProvider>);
  return client;
}
afterEach(() => { clients.splice(0).forEach((client) => client.clear()); vi.clearAllMocks(); vi.useRealTimers(); });

describe("resource readiness wiring", () => {
  it("updates the open editor from live resources and suppresses readiness in in-place mode", async () => {
    const client = setup();
    await screen.findByText("Repository");
    fireEvent.click(screen.getByTitle("Change how runs use this folder"));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("status")).toHaveTextContent("cleanup required");
    act(() => client.setQueryData(projectResourceKeys.list("ws", "p"), {
      resources: [{ ...resource, worktree_readiness: { ...resource.worktree_readiness, status: "ready", file_count: 0, total_bytes: 0, largest_paths: [] } }], total: 1,
    }));
    await waitFor(() => expect(within(dialog).getByRole("status")).toHaveTextContent("Ready"));
    fireEvent.click(within(dialog).getAllByRole("radio")[0]!);
    expect(within(dialog).queryByRole("status")).not.toBeInTheDocument();
  });
  it("polls visible resources every five seconds and pauses when collapsed", async () => {
    setup();
    await screen.findByText("Repository");
    vi.useFakeTimers();
    listResources.mockClear();
    // Re-rendering the observer installs the polling interval under fake timers.
    fireEvent.click(screen.getByRole("button", { name: "Resources" }));
    fireEvent.click(screen.getByRole("button", { name: "Resources" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(listResources).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Resources" }));
    listResources.mockClear();
    await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
    expect(listResources).not.toHaveBeenCalled();
  });
  it("does not show another configuration's measurement in an open editor", async () => {
    const client = setup();
    await screen.findByText("Repository");
    fireEvent.click(screen.getByTitle("Change how runs use this folder"));
    const dialog = await screen.findByRole("dialog");
    act(() => client.setQueryData(projectResourceKeys.list("ws", "p"), {
      resources: [{ ...resource, resource_ref: { ...resource.resource_ref, local_path: "/other-repo" } }], total: 1,
    }));
    await waitFor(() => expect(within(dialog).getByRole("status")).toHaveTextContent("unchecked"));
    expect(within(dialog).queryByText("debug")).not.toBeInTheDocument();
  });
  it("marks edits to an in-place resource unchecked until saved and measured", async () => {
    setup({ ...resource, resource_ref: { ...resource.resource_ref, execution_mode: "in_place" }, worktree_readiness: undefined });
    await screen.findByText("Repository");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTitle("Change how runs use this folder"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getAllByRole("radio")[1]!);
    expect(within(dialog).getByRole("status")).toHaveTextContent("unchecked");
  });
  it("turns cached ready state unavailable after a failed refresh", async () => {
    const client = setup({ ...resource, worktree_readiness: { ...resource.worktree_readiness!, status: "ready" } });
    await screen.findByText("Ready for worktree runs");
    listResources.mockRejectedValue(new Error("offline"));
    await act(async () => { await client.invalidateQueries({ queryKey: projectResourceKeys.list("ws", "p") }); });
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("unavailable"));
  });
});
