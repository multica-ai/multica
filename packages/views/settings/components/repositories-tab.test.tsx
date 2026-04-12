import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type {
  Workspace,
  WorkspaceRepo,
  MemberWithUser,
  Project,
} from "@multica/core/types";

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const baseWorkspace: Workspace = {
  id: "ws-1",
  name: "WS",
  slug: "ws",
  description: null,
  context: null,
  settings: {},
  repos: [
    {
      id: "r-existing",
      name: "legacy",
      type: "github",
      url: "https://github.com/org/legacy.git",
      description: "",
    },
  ],
  issue_prefix: "WS",
  created_at: "2026-04-01T00:00:00Z",
  updated_at: "2026-04-01T00:00:00Z",
};

const mockUpdateWorkspace = vi.fn();

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => {
      const state = { user: { id: "user-1", name: "me", email: "me@x" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: { id: "user-1" } }) },
  ),
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceStore: Object.assign(
    (selector?: any) => {
      const state = {
        workspace: baseWorkspace,
        updateWorkspace: mockUpdateWorkspace,
      };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ workspace: baseWorkspace }) },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: () =>
      Promise.resolve<MemberWithUser[]>([
        {
          id: "m-1",
          workspace_id: "ws-1",
          user_id: "user-1",
          role: "owner",
          created_at: "2026-04-01T00:00:00Z",
          name: "me",
          email: "me@x",
          avatar_url: null,
        },
      ]),
  }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({
    queryKey: ["projects"],
    queryFn: () => Promise.resolve<Project[]>([]),
  }),
}));

const apiUpdateWorkspace = vi.fn(
  async (_id: string, data: { repos?: WorkspaceRepo[] }) => ({
    ...baseWorkspace,
    repos: data.repos ?? baseWorkspace.repos,
  }),
);
vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: (...args: [string, { repos?: WorkspaceRepo[] }]) =>
      apiUpdateWorkspace(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("next/link", () => ({
  __esModule: true,
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// Import AFTER mocks.
import { RepositoriesTab } from "./repositories-tab";

function renderTab() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <RepositoriesTab />
    </QueryClientProvider>,
  );
}

describe("RepositoriesTab", () => {
  beforeEach(() => {
    apiUpdateWorkspace.mockClear();
    mockUpdateWorkspace.mockClear();
  });

  it("renders an existing github repo row with its url and name", () => {
    renderTab();
    expect(screen.getByDisplayValue("legacy")).toBeInTheDocument();
    expect(
      screen.getByDisplayValue("https://github.com/org/legacy.git"),
    ).toBeInTheDocument();
  });

  it("exposes add buttons for both repo types", () => {
    renderTab();
    // Multiple buttons may include these labels; use getAllByText for stability.
    expect(screen.getAllByText(/GitHub/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Local/i).length).toBeGreaterThan(0);
  });

  it("saves edits through api.updateWorkspace once permissions resolve", async () => {
    const { container } = renderTab();
    // The Save button only appears once the member query resolves and
    // canManageWorkspace is true. Wait for it before clicking.
    let saveBtn: HTMLButtonElement | undefined;
    await waitFor(() => {
      const buttons = Array.from(container.querySelectorAll("button"));
      saveBtn = buttons.find(
        (b) => (b as HTMLButtonElement).textContent?.trim() === "Save",
      ) as HTMLButtonElement | undefined;
      expect(saveBtn).toBeDefined();
    });
    fireEvent.click(saveBtn!);
    await waitFor(() => {
      expect(apiUpdateWorkspace).toHaveBeenCalled();
    });
    const call = apiUpdateWorkspace.mock.calls[0];
    expect(call).toBeDefined();
    const [wsId, body] = call!;
    expect(wsId).toBe("ws-1");
    // The initial repo is sent back as-is when user only clicks Save.
    const repos = body.repos!;
    expect(repos).toHaveLength(1);
    expect(repos[0]!.id).toBe("r-existing");
    expect(repos[0]!.type).toBe("github");
  });
});
