import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockGetConnectURL = vi.hoisted(() => vi.fn());
const mockGetClaimURL = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockNavPush = vi.hoisted(() => vi.fn());
const mockNavReplace = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());

const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Acme",
    slug: "acme",
    settings: {} as Record<string, unknown>,
    repos: [{ url: "https://github.com/acme/api" }] as { url: string }[],
  },
}));
type MemberRole = "owner" | "admin" | "member" | "guest";
const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const installationsRef = vi.hoisted(() => ({
  current: {
    installations: [] as {
      id: string;
      account_login: string;
      installation_id?: number;
      connected_by?: string;
    }[],
    configured: true,
    can_manage: true as boolean,
  },
}));
const searchParamsRef = vi.hoisted(() => ({
  current: new URLSearchParams("tab=github"),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current };
    if (key.includes("installations")) return { data: installationsRef.current };
    return { data: undefined };
  },
  useQueryClient: () => ({
    setQueryData: mockSetQueryData,
    invalidateQueries: mockInvalidate,
  }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/github", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/github")>("@multica/core/github");
  return {
    ...actual,
    githubInstallationsOptions: () => ({
      queryKey: ["github", "installations"],
      queryFn: vi.fn(),
    }),
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: mockUpdateWorkspace,
    deleteGitHubInstallation: mockDeleteInstallation,
    getGitHubConnectURL: mockGetConnectURL,
    getGitHubClaimURL: mockGetClaimURL,
  },
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

// Mocked at the context module rather than the barrel so <AppLink> stays the
// real component and its click contract is what the test exercises.
vi.mock("../../navigation/context", () => ({
  useNavigation: () => ({
    push: mockNavPush,
    replace: mockNavReplace,
    back: vi.fn(),
    pathname: "/acme/settings",
    searchParams: searchParamsRef.current,
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn() },
}));

import { GitHubTab } from "./github-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function resetFixtures() {
  vi.clearAllMocks();
  workspaceRef.current = {
    id: "workspace-1",
    name: "Acme",
    slug: "acme",
    settings: {},
    repos: [{ url: "https://github.com/acme/api" }],
  };
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  installationsRef.current = { installations: [], configured: true, can_manage: true };
  searchParamsRef.current = new URLSearchParams("tab=github");
  mockNavReplace.mockImplementation((path: string) => {
    searchParamsRef.current = new URLSearchParams(path.split("?")[1] ?? "");
  });
}

describe("GitHubTab", () => {
  beforeEach(resetFixtures);

  it("folds the non-dev hint into the master switch description (no separate callout)", () => {
    render(<GitHubTab />, { wrapper: I18nWrapper });
    expect(screen.getByText(/Not a development team\? Just turn it off here\./)).toBeTruthy();
    // The old standalone callout (title + dedicated "Turn GitHub off" button) is gone.
    expect(screen.queryByRole("button", { name: /^Turn GitHub off$/ })).toBeNull();
  });

  it("does not show the hint once the master switch is off", () => {
    workspaceRef.current.settings = { github_enabled: false };
    render(<GitHubTab />, { wrapper: I18nWrapper });
    expect(screen.queryByText(/Not a development team\?/)).toBeNull();
  });

  it("disables every feature switch when the master switch is off", () => {
    workspaceRef.current.settings = { github_enabled: false };
    render(<GitHubTab />, { wrapper: I18nWrapper });

    const master = screen.getByRole("switch", { name: /enable github features/i });
    expect(master.getAttribute("aria-checked")).toBe("false");

    const switches = screen.getAllByRole("switch");
    // First switch is master; remaining must be disabled (aria-disabled or disabled attr)
    const features = switches.slice(1);
    expect(features.length).toBeGreaterThan(0);
    for (const sw of features) {
      const ariaDisabled = sw.getAttribute("aria-disabled");
      const disabled = sw.hasAttribute("disabled");
      expect(ariaDisabled === "true" || disabled).toBe(true);
    }
  });

  it("flipping the master switch off persists github_enabled=false and merges existing settings", async () => {
    const user = userEvent.setup();
    workspaceRef.current.settings = { co_authored_by_enabled: true };
    mockUpdateWorkspace.mockResolvedValue({
      ...workspaceRef.current,
      settings: { co_authored_by_enabled: true, github_enabled: false },
    });

    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("switch", { name: /enable github features/i }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
        settings: { co_authored_by_enabled: true, github_enabled: false },
      });
      expect(mockToastSuccess).toHaveBeenCalledWith("Changes saved", {
        id: "settings-auto-save",
      });
    });
  });

  it("clicking Disconnect opens the confirmation and only fires on confirm", async () => {
    const user = userEvent.setup();
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [{ id: "inst-42", account_login: "acme", installation_id: 42 }],
    };
    mockDeleteInstallation.mockResolvedValue(undefined);

    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Disconnect acme" }));
    expect(screen.getByText(/stop receiving webhooks for acme/i)).toBeTruthy();
    expect(mockDeleteInstallation).not.toHaveBeenCalled();

    const dialogConfirm = screen
      .getAllByRole("button", { name: /^Disconnect$/ })
      .find((b) => b.getAttribute("data-slot")?.includes("alert-dialog"));
    await user.click(dialogConfirm ?? screen.getAllByRole("button", { name: /^Disconnect$/ })[1]!);

    await waitFor(() => {
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-42");
    });
  });

  it("disconnects the installation the administrator selected", async () => {
    const user = userEvent.setup();
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        { id: "inst-personal", account_login: "personal", installation_id: 1 },
        { id: "inst-org", account_login: "acme-org", installation_id: 2 },
      ],
    };
    mockDeleteInstallation.mockResolvedValue(undefined);

    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(
      screen.getByRole("button", { name: "Disconnect acme-org" }),
    );
    expect(screen.getByText(/stop receiving webhooks for acme-org/i)).toBeTruthy();
    const confirm = screen
      .getAllByRole("button", { name: /^Disconnect$/ })
      .at(-1)!;
    await user.click(confirm);

    await waitFor(() => {
      expect(mockDeleteInstallation).toHaveBeenCalledWith(
        "workspace-1",
        "inst-org",
      );
      expect(mockDeleteInstallation).not.toHaveBeenCalledWith(
        "workspace-1",
        "inst-personal",
      );
    });
  });

  it("Disconnect button is still visible when the master switch is off", () => {
    workspaceRef.current.settings = { github_enabled: false };
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [{ id: "inst-1", account_login: "acme", installation_id: 1 }],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });
    expect(screen.getByRole("button", { name: "Disconnect acme" })).toBeTruthy();
  });

  it("can connect another account or organization without disconnecting the first", async () => {
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [{ id: "inst-1", account_login: "personal", installation_id: 1 }],
    };
    mockGetConnectURL.mockResolvedValue({
      configured: true,
      url: "https://github.com/apps/multica/installations/new",
    });
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const user = userEvent.setup();
    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(
      screen.getByRole("button", { name: "Add account or organization" }),
    );

    expect(mockGetConnectURL).toHaveBeenCalledWith("workspace-1");
    expect(open).toHaveBeenCalledWith(
      "https://github.com/apps/multica/installations/new",
      "_blank",
      "noopener",
    );
    expect(screen.getByRole("button", { name: "Disconnect personal" })).toBeTruthy();
    open.mockRestore();
  });

  it("explains why additional-installation actions are disabled when GitHub is not configured", () => {
    installationsRef.current = {
      configured: false,
      can_manage: true,
      installations: [{ id: "inst-1", account_login: "personal", installation_id: 1 }],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(
      screen
        .getByRole("button", { name: "Add account or organization" })
        .getAttribute("title"),
    ).toBe("GitHub App is not configured on this server");
    expect(
      screen
        .getByRole("button", { name: "Connect existing installation" })
        .getAttribute("title"),
    ).toBe("GitHub App is not configured on this server");
  });

  it("securely starts a claim for an existing GitHub App installation", async () => {
    mockGetClaimURL.mockResolvedValue({
      configured: true,
      url: "https://github.com/login/oauth/authorize?state=claim",
    });
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const user = userEvent.setup();
    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(
      screen.getByRole("button", { name: "Connect existing installation" }),
    );
    await user.type(
      screen.getByRole("textbox", { name: "GitHub account or organization" }),
      "Acme-Org",
    );
    await user.click(
      screen.getByRole("button", { name: "Authorize and connect" }),
    );

    expect(mockGetClaimURL).toHaveBeenCalledWith(
      "workspace-1",
      "Acme-Org",
      "github",
    );
    expect(open).toHaveBeenCalledWith(
      "https://github.com/login/oauth/authorize?state=claim",
      "_blank",
      "noopener",
    );
    open.mockRestore();
  });

  it("opens the existing-installation form from the repositories shortcut and cleans callback params", async () => {
    searchParamsRef.current = new URLSearchParams(
      "tab=github&github_claim=1&github_connected=1&github_installation=internal-row",
    );

    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(
      screen.getByRole("textbox", { name: "GitHub account or organization" }),
    ).toBeTruthy();
    await waitFor(() => {
      expect(mockNavReplace).toHaveBeenCalledWith("/acme/settings?tab=github");
    });
  });

  it("non-admin sees the existing connection but no Connect/Disconnect controls", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      configured: true,
      can_manage: false,
      installations: [{ id: "inst-1", account_login: "acme" }],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(screen.getByText(/Connected to acme/i)).toBeTruthy();
    expect(screen.getByText(/Read-only view\./i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Connect GitHub$/ })).toBeNull();
    expect(
      screen.queryByRole("button", { name: /^Disconnect / }),
    ).toBeNull();
  });

  it("non-admin with no connection sees the contact-admin hint", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      configured: true,
      can_manage: false,
      installations: [],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(screen.getByText(/Ask an admin or owner/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Connect GitHub$/ })).toBeNull();
  });

  it("renders the connected_by line when the backend provides it", () => {
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-7",
          account_login: "acme",
          installation_id: 7,
          connected_by: "Jiayuan",
        },
      ],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });
    expect(screen.getByText(/Connected by Jiayuan/)).toBeTruthy();
  });

  it("repositories shortcut navigates to the repositories tab", async () => {
    const user = userEvent.setup();
    render(<GitHubTab />, { wrapper: I18nWrapper });
    await user.click(screen.getByRole("button", { name: /Manage repositories/ }));
    expect(mockNavPush).toHaveBeenCalledWith("/acme/settings?tab=repositories");
  });
});
