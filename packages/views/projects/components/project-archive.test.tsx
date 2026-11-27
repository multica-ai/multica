import { useState } from "react";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { projectKeys } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { useArchiveProjects, useRestoreProjects } from "./project-archive";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({ api: { archiveProject: vi.fn(), restoreProject: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const project: Project = {
  id: "p1", workspace_id: "ws", title: "Release", description: null, icon: null,
  status: "planned", priority: "none", lead_type: null, lead_id: null,
  start_date: null, due_date: null, created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z", issue_count: 0, done_count: 0, resource_count: 0,
  archived_at: "2026-01-02T00:00:00Z",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function Restorer({ leave }: { leave: () => void }) {
  const restore = useRestoreProjects();
  return <button onClick={() => { restore(project); leave(); }}>Restore</button>;
}

function Archiver({ leave }: { leave: () => void }) {
  const { requestArchive } = useArchiveProjects();
  return <button onClick={() => requestArchive([{ ...project, archived_at: null }], {
    immediate: true, onArchived: leave,
  })}>Archive</button>;
}

function Leaving({ archive }: { archive: boolean }) {
  const [here, setHere] = useState(true);
  if (!here) return <span>Other page</span>;
  return archive ? <Archiver leave={() => setHere(false)} /> : <Restorer leave={() => setHere(false)} />;
}

function renderFlow(archive = false) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  client.setQueryData(projectKeys.list("ws"), { projects: [project], total: 1 });
  client.setQueryData(projectKeys.detail("ws", project.id), project);
  renderWithI18n(<QueryClientProvider client={client}><Leaving archive={archive} /></QueryClientProvider>);
  return client;
}

describe("project restore feedback", () => {
  beforeEach(() => vi.clearAllMocks());

  it("confirms a restore and updates the cache after leaving the caller", async () => {
    const response = deferred<Project>();
    vi.mocked(api.restoreProject).mockReturnValue(response.promise);
    const client = renderFlow();
    fireEvent.click(screen.getByText("Restore"));
    await waitFor(() => expect(api.restoreProject).toHaveBeenCalledWith(project.id));
    expect(screen.getByText("Other page")).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled();
    await act(async () => response.resolve({ ...project, archived_at: null }));
    expect(toast.success).toHaveBeenCalledWith(expect.stringContaining(project.title));
    expect(client.getQueryData<Project>(projectKeys.detail("ws", project.id))?.archived_at).toBeNull();
  });

  it("reports a rejected restore after leaving the caller", async () => {
    const response = deferred<Project>();
    vi.mocked(api.restoreProject).mockReturnValue(response.promise);
    const client = renderFlow();
    fireEvent.click(screen.getByText("Restore"));
    await waitFor(() => expect(api.restoreProject).toHaveBeenCalled());
    expect(screen.getByText("Other page")).toBeInTheDocument();
    await act(async () => response.reject(new Error("Restore rejected")));
    expect(toast.error).toHaveBeenCalledWith("Restore rejected");
    expect(toast.success).not.toHaveBeenCalled();
    expect(client.getQueryData<Project>(projectKeys.detail("ws", project.id))?.archived_at).toBe(project.archived_at);
  });

  it("reports a failed archive Undo after its caller has left", async () => {
    vi.mocked(api.archiveProject).mockResolvedValue({ ...project, paused_autopilot_ids: [] });
    const response = deferred<Project>();
    vi.mocked(api.restoreProject).mockReturnValue(response.promise);
    const client = renderFlow(true);
    fireEvent.click(screen.getByText("Archive"));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    expect(screen.getByText("Other page")).toBeInTheDocument();
    const action = vi.mocked(toast.success).mock.calls[0]![1]!.action;
    if (!action || typeof action !== "object" || !("onClick" in action)) throw new Error("Undo missing");
    renderWithI18n(<button onClick={action.onClick}>Undo</button>);
    fireEvent.click(screen.getByText("Undo"));
    await waitFor(() => expect(api.restoreProject).toHaveBeenCalledWith(project.id));
    await act(async () => response.reject(new Error("Undo restore rejected")));
    expect(toast.error).toHaveBeenCalledWith("Undo restore rejected");
    expect(client.getQueryData<Project>(projectKeys.detail("ws", project.id))?.archived_at).toBe(project.archived_at);
  });
});
