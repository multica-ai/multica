import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NavigationProvider } from "../navigation";
import type { NavigationAdapter } from "../navigation/types";
import type { Workspace } from "@multica/core/types";
import { WorkspaceRail } from "./workspace-rail";

const listWorkspaces = vi.fn<() => Promise<Workspace[]>>();

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: () => listWorkspaces(),
  },
}));

const openModal = vi.fn();
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: openModal }),
    { getState: () => ({ open: openModal }) },
  ),
}));

function makeWorkspace(over: Partial<Workspace> = {}): Workspace {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    name: "Acme",
    slug: "acme",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACM",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

function renderRail({
  pathname = "/acme/issues",
  workspaces = [makeWorkspace()],
}: {
  pathname?: string;
  workspaces?: Workspace[];
} = {}) {
  listWorkspaces.mockResolvedValueOnce(workspaces);
  // Disable retries so the query doesn't churn after a successful response.
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // Pre-seed the cache so the query resolves synchronously and we don't
  // need to await for skeleton → data transitions in the happy-path tests.
  qc.setQueryData(["workspaces", "list"], workspaces);
  const navigation: NavigationAdapter = {
    pathname,
    searchParams: new URLSearchParams(),
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
  };
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={navigation}>
        <WorkspaceRail />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

describe("WorkspaceRail", () => {
  beforeEach(() => {
    listWorkspaces.mockReset();
    openModal.mockReset();
  });

  it("renders the All workspaces tile, one tile per workspace, and a New button", () => {
    renderRail({
      workspaces: [
        makeWorkspace({ id: "00000000-0000-0000-0000-000000000001", name: "Acme", slug: "acme" }),
        makeWorkspace({ id: "00000000-0000-0000-0000-000000000002", name: "Beta", slug: "beta" }),
      ],
    });

    expect(screen.getByLabelText("All workspaces")).toBeInTheDocument();
    expect(screen.getByLabelText("Acme")).toBeInTheDocument();
    expect(screen.getByLabelText("Beta")).toBeInTheDocument();
    expect(screen.getByLabelText("Create workspace")).toBeInTheDocument();
  });

  it("marks the All workspaces tile active on /global routes", () => {
    renderRail({ pathname: "/global", workspaces: [makeWorkspace()] });
    expect(screen.getByLabelText("All workspaces")).toHaveAttribute("aria-current", "page");
    expect(screen.getByLabelText("Acme")).not.toHaveAttribute("aria-current");
  });

  it("marks the matching workspace tile active when the URL slug matches", () => {
    renderRail({
      pathname: "/beta/issues/ABC-1",
      workspaces: [
        makeWorkspace({ id: "00000000-0000-0000-0000-000000000001", name: "Acme", slug: "acme" }),
        makeWorkspace({ id: "00000000-0000-0000-0000-000000000002", name: "Beta", slug: "beta" }),
      ],
    });
    expect(screen.getByLabelText("Beta")).toHaveAttribute("aria-current", "page");
    expect(screen.getByLabelText("Acme")).not.toHaveAttribute("aria-current");
    expect(screen.getByLabelText("All workspaces")).not.toHaveAttribute("aria-current");
  });

  it("links each workspace tile to that workspace's issues page", () => {
    renderRail({
      workspaces: [makeWorkspace({ slug: "acme" })],
    });
    expect(screen.getByLabelText("Acme")).toHaveAttribute("href", "/acme/issues");
    expect(screen.getByLabelText("All workspaces")).toHaveAttribute("href", "/global");
  });

  it("opens the create-workspace modal when the New button is clicked", () => {
    renderRail();
    fireEvent.click(screen.getByLabelText("Create workspace"));
    expect(openModal).toHaveBeenCalledWith("create-workspace");
  });

  it("renders skeleton tiles while the workspace list is loading", () => {
    // No pre-seed: the query resolves on the next tick so the first paint
    // is the loading state.
    listWorkspaces.mockImplementation(
      () => new Promise<Workspace[]>(() => {}),
    );
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const navigation: NavigationAdapter = {
      pathname: "/global",
      searchParams: new URLSearchParams(),
      push: vi.fn(),
      replace: vi.fn(),
      back: vi.fn(),
    };
    render(
      <QueryClientProvider client={qc}>
        <NavigationProvider value={navigation}>
          <WorkspaceRail />
        </NavigationProvider>
      </QueryClientProvider>,
    );
    // Anchor: All workspaces + New button render even during loading.
    expect(screen.getByLabelText("All workspaces")).toBeInTheDocument();
    expect(screen.getByLabelText("Create workspace")).toBeInTheDocument();
    // No workspace tiles yet — skeletons only.
    expect(screen.queryByLabelText("Acme")).toBeNull();
  });
});
