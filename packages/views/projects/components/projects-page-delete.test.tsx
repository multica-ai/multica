import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Project } from "@multica/core/types";
import { projectKeys } from "@multica/core/projects/queries";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import { ProjectsPage } from "./projects-page";

// Keep the page, project queries, mutation and React subscriptions real.
// Stub unrelated navigation, membership and pin boundaries.
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "User",
    getActorInitials: () => "U",
    getActorAvatarUrl: () => null,
  }),
}));
vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"], queryFn: async () => [] }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const project: Project = {
  id: "project-1",
  workspace_id: "workspace-1",
  title: "Launch Plan",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "high",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "2026-06-01T00:00:00Z",
  updated_at: "2026-06-01T00:00:00Z",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};
const listKey = projectKeys.list("workspace-1");
const detailKey = projectKeys.detail("workspace-1", project.id);

function deferred() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("ProjectsPage deletion lifecycle", () => {
  let qc: QueryClient;
  let pending: ReturnType<typeof deferred>;
  const deleteProject = vi.fn();
  const listProjects = vi.fn();

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: {
        queries: { staleTime: Infinity, retry: false },
        mutations: { retry: false },
      },
    });
    pending = deferred();
    deleteProject.mockReset().mockReturnValue(pending.promise);
    listProjects
      .mockReset()
      .mockResolvedValue({ projects: [project], total: 1 });
    setApiInstance({ deleteProject, listProjects } as unknown as ApiClient);
    qc.setQueryData(listKey, { projects: [project], total: 1 });
    qc.setQueryData(detailKey, project);
    qc.setQueryData(
      ["members"],
      [{ user_id: "user-1", name: "User", role: "admin" }],
    );
    qc.setQueryData(["agents"], []);
    qc.setQueryData(["pins"], []);
    vi.mocked(toast.error).mockClear();
  });

  afterEach(() => {
    onlineManager.setOnline(true);
    qc.clear();
  });

  function renderPage() {
    return renderWithI18n(
      <QueryClientProvider client={qc}>
        <NavigationProvider
          value={{
            push: vi.fn(),
            replace: vi.fn(),
            back: vi.fn(),
            pathname: "/test/projects",
            searchParams: new URLSearchParams(),
            hash: "",
            getShareableUrl: (p) => p,
          }}
        >
          <ProjectsPage />
        </NavigationProvider>
      </QueryClientProvider>,
    );
  }

  async function confirmDeletion() {
    const user = userEvent.setup();
    fireEvent.keyDown(screen.getByRole("button", { name: "Project actions" }), {
      key: "ArrowDown",
    });
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );
  }

  it.each([
    ["403", Object.assign(new Error("Forbidden"), { status: 403 })],
    ["network error", new TypeError("Failed to fetch")],
  ])(
    "keeps the row mounted and reports %s after a confirmed delete",
    async (_label, error) => {
      renderPage();
      await confirmDeletion();
      await waitFor(() =>
        expect(deleteProject).toHaveBeenCalledWith(project.id),
      );
      // Settling before this assertion also verifies the actual row's error callback.
      await act(async () => pending.reject(error));
      await waitFor(() =>
        expect(toast.error).toHaveBeenCalledWith(error.message),
      );
      expect(screen.getByText(project.title)).toBeInTheDocument();
      expect(qc.getQueryData(detailKey)).toEqual(project);
    },
  );

  it("keeps the row visible offline and removes it only after server success", async () => {
    onlineManager.setOnline(false);
    renderPage();
    await confirmDeletion();
    await waitFor(() =>
      expect(qc.getMutationCache().getAll()[0]?.state.isPaused).toBe(true),
    );
    expect(deleteProject).not.toHaveBeenCalled();
    expect(screen.getByText(project.title)).toBeInTheDocument();
    expect(qc.getQueryData(detailKey)).toEqual(project);
    act(() => onlineManager.setOnline(true));
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    expect(screen.getByText(project.title)).toBeInTheDocument();
    listProjects.mockResolvedValue({ projects: [], total: 0 });
    await act(async () => pending.resolve());
    await waitFor(() =>
      expect(screen.queryByText(project.title)).not.toBeInTheDocument(),
    );
    expect(qc.getQueryState(detailKey)).toBeUndefined();
  });

  it("cleans confirmed deletion caches after the list page unmounts", async () => {
    const page = renderPage();
    await confirmDeletion();
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    page.unmount();
    await act(async () => pending.resolve());
    await waitFor(() =>
      expect(qc.getQueryData(listKey)).toEqual({ projects: [], total: 0 }),
    );
    expect(qc.getQueryState(detailKey)).toBeUndefined();
  });
});
