import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockGetConnectURL = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Acme",
    slug: "acme",
    issue_prefix: "ACM",
    settings: {} as Record<string, unknown>,
  },
}));
const roleRef = vi.hoisted(() => ({
  current: "owner" as "owner" | "admin" | "member",
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as { id: string; account_login: string; connected_by?: string }[],
    configured: true,
    can_manage: true as boolean,
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[] }) =>
    JSON.stringify(opts.queryKey).includes("installations")
      ? { data: installationsRef.current }
      : { data: [] },
  useQueryClient: () => ({ setQueryData: vi.fn(), invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => workspaceRef.current }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({
    role: roleRef.current,
    member: { role: roleRef.current },
    isLoading: false,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));
vi.mock("@multica/core/github", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/github")>(
    "@multica/core/github",
  );
  return {
    ...actual,
    githubInstallationsOptions: () => ({ queryKey: ["github", "installations"] }),
  };
});
vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    deleteGitHubInstallation: mockDeleteInstallation,
    getGitHubConnectURL: mockGetConnectURL,
  },
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children: ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=code"),
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
// The repository list and self-hosted rows have their own tests.
vi.mock("./repositories-section", () => ({ RepositoriesSection: () => null }));
vi.mock("./code-vcs", () => ({ VCSConnectionRows: () => <div>VCS rows</div> }));
vi.mock("./pr-merge-status-row", () => ({
  usePRMergeStatus: () => ({ value: "done", renderOption: (key: string) => <span>{key}</span> }),
}));

import { CodeTab } from "./code-tab";

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
    name: "Acme",
    slug: "acme",
    issue_prefix: "ACM",
    settings: { pr_merge_status: "done" },
  };
  roleRef.current = "owner";
  installationsRef.current = { installations: [], configured: true, can_manage: true };
  configStore
    .getState()
    .setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: false });
  mockUpdateWorkspace.mockImplementation(async (_id: string, payload: object) => ({
    ...workspaceRef.current,
    ...payload,
  }));
});

function connect() {
  installationsRef.current = {
    installations: [{ id: "inst-1", account_login: "acme", connected_by: "Ada" }],
    configured: true,
    can_manage: true,
  };
}

describe("CodeTab — GitHub", () => {
  it("offers to connect GitHub and opens the install flow", async () => {
    mockGetConnectURL.mockResolvedValue({ configured: true, url: "https://github.com/apps/x" });
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const user = userEvent.setup();
    render(<CodeTab />, { wrapper: Wrapper });

    expect(screen.getByText("Not connected")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Connect GitHub" }));

    await waitFor(() =>
      expect(open).toHaveBeenCalledWith("https://github.com/apps/x", "_blank", "noopener"),
    );
    open.mockRestore();
  });

  it("names the account and who connected it", () => {
    connect();
    render(<CodeTab />, { wrapper: Wrapper });

    expect(screen.getByText("Connected")).toBeInTheDocument();
    expect(screen.getByText(/Connected to acme · Connected by Ada/)).toBeInTheDocument();
  });

  it("pauses GitHub from the row menu, keeping the other settings", async () => {
    connect();
    workspaceRef.current.settings = { pr_merge_status: "done", co_authored_by_enabled: false };
    const user = userEvent.setup();
    render(<CodeTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "GitHub actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Pause GitHub features" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        settings: {
          pr_merge_status: "done",
          co_authored_by_enabled: false,
          github_enabled: false,
        },
      });
    });
  });

  it("disables the PR switches while GitHub is paused", () => {
    connect();
    workspaceRef.current.settings = { github_enabled: false };
    render(<CodeTab />, { wrapper: Wrapper });

    expect(screen.getByText("Paused")).toBeInTheDocument();
    for (const name of ["Pull Request sidebar", "Auto-link issues and PRs", "Co-authored-by trailer"]) {
      expect(screen.getByRole("switch", { name })).toHaveAttribute("aria-disabled", "true");
    }
  });

  it("disconnects only after confirmation", async () => {
    connect();
    mockDeleteInstallation.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<CodeTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: "GitHub actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Disconnect" }));
    expect(mockDeleteInstallation).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Disconnect" }));

    await waitFor(() =>
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-1"),
    );
  });

  it("shows members the connection read-only and who can change it", () => {
    connect();
    roleRef.current = "member";
    installationsRef.current.can_manage = false;
    render(<CodeTab />, { wrapper: Wrapper });

    expect(screen.getByRole("note")).toHaveTextContent("View only");
    expect(screen.queryByRole("button", { name: "GitHub actions" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Pull Request sidebar" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});

describe("CodeTab — pull requests", () => {
  it("saves a PR switch into the workspace settings", async () => {
    connect();
    const user = userEvent.setup();
    render(<CodeTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("switch", { name: "Co-authored-by trailer" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        settings: { pr_merge_status: "done", co_authored_by_enabled: false },
      });
    });
  });

  it("uses the workspace prefix in the auto-link example", () => {
    render(<CodeTab />, { wrapper: Wrapper });
    expect(screen.getByText(/ACM-123/)).toBeInTheDocument();
  });

  it("shows the merge rule here but edits it on Statuses & transitions", () => {
    render(<CodeTab />, { wrapper: Wrapper });

    expect(screen.getByText("done")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Change in Statuses & transitions/ }),
    ).toHaveAttribute("href", "/acme/settings?tab=issue-statuses&section=pr-merge-status");
    expect(screen.queryByRole("combobox")).toBeNull();
  });

  it("lists self-hosted Git only where the deployment supports it", () => {
    const { unmount } = render(<CodeTab />, { wrapper: Wrapper });
    expect(screen.queryByText("VCS rows")).toBeNull();
    unmount();

    configStore
      .getState()
      .setAuthConfig({ allowSignup: true, vcsIntegrationAvailable: true });
    render(<CodeTab />, { wrapper: Wrapper });
    expect(screen.getByText("VCS rows")).toBeInTheDocument();
  });
});
