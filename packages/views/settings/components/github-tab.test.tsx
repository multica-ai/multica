import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { GitHubInstallation } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockDeleteInstallation = vi.hoisted(() => vi.fn());
const mockGetConnectURL = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());
const mockNavPush = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

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
    installations: [] as GitHubInstallation[],
    configured: true,
    can_manage: true as boolean,
  },
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

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: mockNavPush,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=github"),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
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
}

describe("GitHubTab", () => {
  beforeEach(resetFixtures);
  afterEach(() => vi.unstubAllGlobals());

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

  it("renders every installation separately and keeps Connect another available", async () => {
    class LoadedImage {
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;
      referrerPolicy = "";
      crossOrigin: string | null = null;

      set src(_value: string) {
        queueMicrotask(() => this.onload?.());
      }
    }
    vi.stubGlobal("Image", LoadedImage);

    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-user",
          workspace_id: "workspace-1",
          account_login: "octocat",
          account_type: "User",
          account_avatar_url: "https://avatars.example/octocat.png",
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 41,
        },
        {
          id: "inst-org",
          workspace_id: "workspace-1",
          account_login: "acme-org",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 42,
          connected_by: "Jiayuan",
        },
      ],
    };

    const { container } = render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("octocat")).toBeTruthy();
    expect(screen.getByText("acme-org")).toBeTruthy();
    expect(screen.getByText("Personal account")).toBeTruthy();
    expect(screen.getByText("Organization")).toBeTruthy();
    expect(screen.queryByText(/octocat, acme-org/)).toBeNull();
    expect(screen.getByRole("button", { name: "Connect another GitHub" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Disconnect octocat" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Disconnect acme-org" })).toBeTruthy();
    expect(container.querySelectorAll('[data-slot="avatar"]')).toHaveLength(2);
    await waitFor(() => {
      expect(
        container.querySelector('img[src="https://avatars.example/octocat.png"]'),
      ).toBeTruthy();
    });
    expect(container.querySelectorAll('[data-slot="avatar-fallback"]')).toHaveLength(1);
  });

  it("renders an explicit fallback for a future GitHub account type", () => {
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-future",
          workspace_id: "workspace-1",
          account_login: "future-account",
          account_type: "Enterprise",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
        },
      ],
    };

    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("Unknown account type")).toBeTruthy();
  });

  it("disconnects the selected installation row", async () => {
    const user = userEvent.setup();
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-first",
          workspace_id: "workspace-1",
          account_login: "octocat",
          account_type: "User",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 41,
        },
        {
          id: "inst-second",
          workspace_id: "workspace-1",
          account_login: "acme-org",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 42,
        },
      ],
    };
    mockDeleteInstallation.mockResolvedValue(undefined);

    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Disconnect acme-org" }));
    expect(screen.getByRole("heading", { name: "Disconnect acme-org?" })).toBeTruthy();
    expect(mockDeleteInstallation).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: /^Disconnect$/ }));

    await waitFor(() => {
      expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-second");
    });
  });

  it("keeps the selected installation dialog open when disconnect fails", async () => {
    const user = userEvent.setup();
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-failing",
          workspace_id: "workspace-1",
          account_login: "acme-failing",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 43,
        },
      ],
    };
    mockDeleteInstallation.mockRejectedValue(new Error("disconnect failed"));

    render(<GitHubTab />, { wrapper: I18nWrapper });

    await user.click(screen.getByRole("button", { name: "Disconnect acme-failing" }));
    await user.click(screen.getByRole("button", { name: /^Disconnect$/ }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("disconnect failed");
    });
    expect(screen.getByRole("heading", { name: "Disconnect acme-failing?" })).toBeTruthy();
  });

  it("Disconnect button is still visible when the master switch is off", () => {
    workspaceRef.current.settings = { github_enabled: false };
    installationsRef.current = {
      configured: true,
      can_manage: true,
      installations: [
        {
          id: "inst-1",
          workspace_id: "workspace-1",
          account_login: "acme",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
          installation_id: 1,
        },
      ],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });
    expect(screen.getByRole("button", { name: "Disconnect acme" })).toBeTruthy();
  });

  it("non-admin sees the existing connection but no Connect/Disconnect controls", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationsRef.current = {
      configured: true,
      can_manage: false,
      installations: [
        {
          id: "inst-1",
          workspace_id: "workspace-1",
          account_login: "octocat",
          account_type: "User",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
        },
        {
          id: "inst-2",
          workspace_id: "workspace-1",
          account_login: "acme-org",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
        },
      ],
    };
    render(<GitHubTab />, { wrapper: I18nWrapper });

    expect(screen.getByText("octocat")).toBeTruthy();
    expect(screen.getByText("acme-org")).toBeTruthy();
    expect(screen.getByText("Personal account")).toBeTruthy();
    expect(screen.getByText("Organization")).toBeTruthy();
    expect(screen.getByText(/Read-only view\./i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Connect/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Disconnect/ })).toBeNull();
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
          workspace_id: "workspace-1",
          account_login: "acme",
          account_type: "Organization",
          account_avatar_url: null,
          created_at: "2026-07-18T00:00:00Z",
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
