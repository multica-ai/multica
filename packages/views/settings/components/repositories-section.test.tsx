import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockGetGitHubConnectURL = vi.hoisted(() => vi.fn());
const mockNavReplace = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [] as { url: string; description?: string }[],
  },
}));
const roleRef = vi.hoisted(() => ({
  current: "owner" as "owner" | "admin" | "member",
}));
const githubRef = vi.hoisted(() => ({
  current: {
    installations: [] as { id: string; account_login: string }[],
    configured: true,
    repository_browse_configured: true,
    can_manage: true,
  },
}));
const githubQueryStateRef = vi.hoisted(() => ({
  current: { isPending: false, isFetching: false },
}));
const githubRepositoriesRef = vi.hoisted(() => ({
  current: [] as {
    id: number;
    full_name: string;
    html_url: string;
    clone_url: string;
    description: string | null;
    private: boolean;
    archived: boolean;
    default_branch: string;
  }[],
}));
const searchParamsRef = vi.hoisted(() => ({
  current: new URLSearchParams("tab=code"),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: githubRef.current, ...githubQueryStateRef.current }),
  useInfiniteQuery: () => ({
    data: {
      pages: [
        {
          repositories: githubRepositoriesRef.current,
          total_count: githubRepositoriesRef.current.length,
          next_page: null,
        },
      ],
    },
    isPending: false,
    isError: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(),
  }),
  useQueryClient: () => ({ setQueryData: vi.fn() }),
  queryOptions: <T,>(options: T) => options,
  infiniteQueryOptions: <T,>(options: T) => options,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: roleRef.current, isLoading: false }),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));
vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    getGitHubConnectURL: mockGetGitHubConnectURL,
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: mockToastError } }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: mockNavReplace,
    pathname: "/acme/settings",
    searchParams: searchParamsRef.current,
  }),
}));

import { RepositoriesSection, repositoryIdentity } from "./repositories-section";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  workspaceRef.current = {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [{ url: "https://github.com/multica-ai/multica", description: "Main app" }],
  };
  roleRef.current = "owner";
  githubRef.current = {
    installations: [],
    configured: true,
    repository_browse_configured: true,
    can_manage: true,
  };
  githubQueryStateRef.current = { isPending: false, isFetching: false };
  githubRepositoriesRef.current = [];
  searchParamsRef.current = new URLSearchParams("tab=code");
  mockUpdateWorkspace.mockImplementation(async (_id: string, payload: object) => ({
    ...workspaceRef.current,
    ...payload,
  }));
});

async function openAddMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Add repository/ }));
}

describe("RepositoriesSection", () => {
  it("lists repositories as read-only rows with their description", () => {
    render(<RepositoriesSection />, { wrapper: Wrapper });

    expect(screen.getByText("https://github.com/multica-ai/multica")).toBeInTheDocument();
    expect(screen.getByText("Main app")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("adds a repository from the dialog in one request", async () => {
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await openAddMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Enter a URL..." }));
    const dialog = await screen.findByRole("dialog", { name: "Add repository" });
    const save = within(dialog).getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();

    await user.type(
      within(dialog).getByRole("textbox", { name: "Repository URL" }),
      "git@github.com:acme/api.git",
    );
    await user.type(
      within(dialog).getByRole("textbox", { name: "Description" }),
      "  Go API  ",
    );
    await user.click(save);

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [
          { url: "https://github.com/multica-ai/multica", description: "Main app" },
          { url: "git@github.com:acme/api.git", description: "Go API" },
        ],
      });
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("refuses a repository that is already listed under another URL form", async () => {
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await openAddMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Enter a URL..." }));
    const dialog = await screen.findByRole("dialog", { name: "Add repository" });
    await user.type(
      within(dialog).getByRole("textbox", { name: "Repository URL" }),
      "git@github.com:multica-ai/multica.git",
    );

    expect(within(dialog).getByText("This repository is already added.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("keeps the dialog open with the input when saving fails", async () => {
    mockUpdateWorkspace.mockRejectedValueOnce(new Error("boom"));
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Actions for https:\/\/github.com\/multica-ai\/multica/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit repository" });
    const description = within(dialog).getByRole("textbox", { name: "Description" });
    await user.clear(description);
    await user.type(description, "Renamed");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("boom"));
    expect(screen.getByRole("dialog", { name: "Edit repository" })).toBeInTheDocument();
    expect(description).toHaveValue("Renamed");
  });

  it("removes a repository only after confirmation", async () => {
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: /Actions for https:\/\/github.com\/multica-ai\/multica/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete repository" }));
    expect(mockUpdateWorkspace).not.toHaveBeenCalled();
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Delete repository",
      }),
    );

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", { repos: [] });
    });
  });

  it("hides every write affordance from members", () => {
    roleRef.current = "member";
    render(<RepositoriesSection />, { wrapper: Wrapper });

    expect(screen.queryByRole("button", { name: /Add repository/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
  });

  it("starts GitHub connection with the signed repository return target", async () => {
    mockGetGitHubConnectURL.mockResolvedValue({
      configured: true,
      url: "https://github.com/apps/multica/installations/new",
    });
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await openAddMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: /Connect GitHub/ }));

    await waitFor(() => {
      expect(mockGetGitHubConnectURL).toHaveBeenCalledWith("workspace-1", "repositories");
      expect(open).toHaveBeenCalledWith(
        "https://github.com/apps/multica/installations/new",
        "_blank",
        "noopener",
      );
    });
    open.mockRestore();
  });

  it("explains why GitHub import is unavailable when browsing is not configured", async () => {
    githubRef.current = { ...githubRef.current, repository_browse_configured: false };
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await openAddMenu(user);
    const item = await screen.findByRole("menuitem", { name: /Connect GitHub/ });
    expect(item).toHaveAttribute("aria-disabled", "true");
    expect(item).toHaveTextContent("GITHUB_APP_ID");
  });

  it("imports selected GitHub repositories and deduplicates HTTPS against SSH", async () => {
    workspaceRef.current = {
      ...workspaceRef.current,
      repos: [{ url: "git@github.com:multica-ai/multica.git" }],
    };
    githubRef.current = {
      ...githubRef.current,
      installations: [{ id: "installation-row-1", account_login: "multica-ai" }],
    };
    githubRepositoriesRef.current = [
      {
        id: 1,
        full_name: "multica-ai/multica",
        html_url: "https://github.com/multica-ai/multica",
        clone_url: "https://github.com/multica-ai/multica.git",
        description: "Existing repository",
        private: false,
        archived: false,
        default_branch: "main",
      },
      {
        id: 2,
        full_name: "multica-ai/console",
        html_url: "https://github.com/multica-ai/console",
        clone_url: "https://github.com/multica-ai/console.git",
        description: "Console app",
        private: true,
        archived: false,
        default_branch: "main",
      },
    ];
    const user = userEvent.setup();
    render(<RepositoriesSection />, { wrapper: Wrapper });

    await openAddMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: /Choose from GitHub/ }));
    const checkboxes = await screen.findAllByRole("checkbox");
    expect(checkboxes).toHaveLength(2);
    expect(
      checkboxes[0]!.hasAttribute("disabled") ||
        checkboxes[0]!.getAttribute("aria-disabled") === "true",
    ).toBe(true);

    await user.click(checkboxes[1]!);
    await user.click(screen.getByRole("button", { name: "Add repositories" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        repos: [
          { url: "git@github.com:multica-ai/multica.git" },
          { url: "https://github.com/multica-ai/console.git", description: "Console app" },
        ],
      });
    });
  });

  it("opens the picker after returning from a GitHub connection", async () => {
    githubRef.current = {
      ...githubRef.current,
      installations: [{ id: "installation-row-1", account_login: "multica-ai" }],
    };
    searchParamsRef.current = new URLSearchParams("tab=repositories&github_connected=1");

    render(<RepositoriesSection />, { wrapper: Wrapper });

    expect(
      await screen.findByRole("heading", { name: "Choose GitHub repositories" }),
    ).toBeTruthy();
    expect(mockNavReplace).toHaveBeenCalledWith("/acme/settings?tab=repositories");
  });

  it("clears the GitHub callback query after an empty installation result", async () => {
    searchParamsRef.current = new URLSearchParams("tab=repositories&github_connected=1");

    render(<RepositoriesSection />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(mockNavReplace).toHaveBeenCalledWith("/acme/settings?tab=repositories");
    });
    expect(screen.queryByRole("heading", { name: "Choose GitHub repositories" })).toBeNull();
  });
});

describe("repositoryIdentity", () => {
  it("preserves repository path casing when comparing clone URLs", () => {
    expect(repositoryIdentity("https://GitHub.com/Acme/Repo.git")).toBe("github.com/Acme/Repo");
    expect(repositoryIdentity("git@github.com:acme/repo.git")).toBe("github.com/acme/repo");
  });
});
