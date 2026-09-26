import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SidebarProvider, useSidebar } from "@multica/ui/components/ui/sidebar";
import { configStore } from "@multica/core/config";
import {
  BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
  PLUGINS_V1_FLAG,
} from "@multica/core/feature-flags";
import { renderWithI18n } from "../../test/i18n";

// This file tests the settings SHELL — the chrome around the tabs — so every
// tab panel is stubbed out. Their contents have their own test files.
const stub = vi.hoisted(() => (name: string) => () => ({
  [name]: () => <div>{name}</div>,
}));
vi.mock("./account-tab", stub("AccountTab"));
vi.mock("./preferences-tab", stub("PreferencesTab"));
vi.mock("./tokens-tab", stub("TokensTab"));
vi.mock("./workspace-tab", stub("WorkspaceTab"));
vi.mock("./members-tab", stub("MembersTab"));
vi.mock("./code-tab", stub("CodeTab"));
vi.mock("./channels-tab", stub("ChannelsTab"));
vi.mock("./notifications-tab", stub("NotificationsTab"));
vi.mock("./labels-tab", stub("LabelsTab"));
vi.mock("./issue-statuses-tab", stub("IssueStatusesTab"));
vi.mock("./properties-tab", stub("PropertiesTab"));
vi.mock("./quick-actions-tab", stub("QuickActionsTab"));
vi.mock("./keyboard-shortcuts-tab", stub("KeyboardShortcutsTab"));
vi.mock("./plugins-tab", stub("PluginsTab"));
vi.mock("./mcp-tab", stub("McpTab"));
vi.mock("./billing-tab", stub("BillingTab"));

const shell = vi.hoisted(() => ({
  appsAvailable: false,
  role: "owner" as "owner" | "admin" | "member",
}));
vi.mock("./connected-apps-tab", () => ({
  ConnectedAppsTab: () => <div>ConnectedAppsTab</div>,
  useComposioAvailable: () => shell.appsAvailable,
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null | undefined) => url ?? null,
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: shell.role, isLoading: false }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1", name: "Ada Lovelace", avatar_url: null } };
  const useAuthStore = Object.assign(
    (selector?: (s: typeof state) => unknown) =>
      selector ? selector(state) : state,
    { getState: () => state },
  );
  return { useAuthStore };
});

const replace = vi.fn();
const push = vi.fn();
const navigationState = { search: "" };
vi.mock("../../navigation/context", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../navigation/context")>()),
  useNavigation: () => ({
    searchParams: new URLSearchParams(navigationState.search),
    hash: "",
    pathname: "/acme/settings",
    replace,
    push,
  }),
}));

// Compact by default: that is the width where the nav is a sheet and this
// trigger is the only way to reach it.
const layout = { compact: true };
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.compact,
  useIsCompact: () => layout.compact,
}));

import { SettingsPage } from "./settings-page";

function NavStateProbe() {
  const { openMobile } = useSidebar();
  return <div data-testid="nav-open">{String(openMobile)}</div>;
}

function trigger() {
  return screen.getByRole("button", { name: "Toggle left sidebar" });
}

function nav() {
  return screen.getByRole("navigation", { name: "Settings" });
}

beforeEach(() => {
  layout.compact = true;
  navigationState.search = "";
  shell.appsAvailable = false;
  shell.role = "owner";
  configStore.getState().setFeatureFlags({});
  replace.mockClear();
  push.mockClear();
});

describe("SettingsPage nav trigger", () => {
  it("opens the nav from settings at compact widths", () => {
    // Settings builds its own chrome instead of a PageHeader, so without this
    // control a touch user who lands here has no way back to the nav at all —
    // the keyboard shortcut is not an answer on a tablet.
    renderWithI18n(
      <SidebarProvider>
        <NavStateProbe />
        <SettingsPage />
      </SidebarProvider>,
    );

    expect(screen.getByTestId("nav-open").textContent).toBe("false");

    fireEvent.click(trigger());

    expect(screen.getByTestId("nav-open").textContent).toBe("true");
  });

  it("hides the trigger only where the nav is a permanent column", () => {
    // The nav is in-flow from `xl` up, so the control is CSS-gated rather than
    // unmounted — jsdom applies no stylesheet, hence the class assertion.
    renderWithI18n(
      <SidebarProvider>
        <SettingsPage />
      </SidebarProvider>,
    );

    expect(trigger().className).toContain("xl:hidden");
  });

  it("still renders standalone, without a sidebar around it", () => {
    // Desktop mounts settings inside its own shell; the trigger has to no-op
    // rather than throw when there is no SidebarProvider above it.
    renderWithI18n(<SettingsPage />);

    expect(
      screen.queryByRole("button", { name: "Toggle left sidebar" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Settings" })).toBeInTheDocument();
  });
});

describe("SettingsPage Plugin feature flag", () => {
  it("hides Plugins and falls back from a direct tab URL when disabled", () => {
    navigationState.search = "tab=plugins";

    renderWithI18n(<SettingsPage />);

    expect(
      screen.queryByRole("link", { name: "Plugins" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("PluginsTab")).not.toBeInTheDocument();
    expect(screen.getByText("AccountTab")).toBeInTheDocument();
  });

  it("shows and mounts Plugins when explicitly enabled", () => {
    navigationState.search = "tab=plugins";
    configStore.getState().setFeatureFlags({ [PLUGINS_V1_FLAG]: true });

    renderWithI18n(<SettingsPage />);

    expect(screen.getByRole("link", { name: "Plugins" })).toBeInTheDocument();
    expect(screen.getByText("PluginsTab")).toBeInTheDocument();
  });
});

describe("SettingsPage workspace subscription feature flag", () => {
  it("hides Billing and falls back to Workspace General from a direct URL", () => {
    navigationState.search = "tab=billing";

    renderWithI18n(<SettingsPage />);

    expect(
      screen.queryByRole("link", { name: "Billing" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("BillingTab")).not.toBeInTheDocument();
    expect(screen.getByText("WorkspaceTab")).toBeInTheDocument();
  });

  it("shows and mounts Billing only when explicitly enabled", () => {
    navigationState.search = "tab=billing";
    configStore.getState().setFeatureFlags({
      [BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG]: true,
    });

    renderWithI18n(<SettingsPage />);

    expect(screen.getByRole("link", { name: "Billing" })).toBeInTheDocument();
    expect(screen.getByText("BillingTab")).toBeInTheDocument();
  });
});

describe("SettingsPage information architecture", () => {
  it("groups pages by who they affect, then by topic", () => {
    renderWithI18n(<SettingsPage />);
    const personal = within(nav()).getByRole("region", { name: /Personal/ });
    expect(within(personal).getByRole("link", { name: "Preferences" })).toBeInTheDocument();
    expect(within(personal).getByRole("link", { name: "API Tokens" })).toBeInTheDocument();

    // The workspace group is named after the workspace it configures.
    const workspace = within(nav()).getByRole("region", { name: /Acme/ });
    const issues = within(workspace).getByRole("group", { name: "Issues & workflow" });
    expect(
      within(issues).getByRole("link", { name: "Statuses & transitions" }),
    ).toBeInTheDocument();
    const connections = within(workspace).getByRole("group", {
      name: "Connections & extensions",
    });
    for (const name of ["Code", "Messaging", "MCP servers"]) {
      expect(within(connections).getByRole("link", { name })).toBeInTheDocument();
    }
  });

  it("keeps retired pages out of navigation", () => {
    renderWithI18n(<SettingsPage />);
    expect(
      within(nav()).queryByRole("link", {
        name: /^(Issue|Chat|GitHub|Labs|Integrations|Repositories)$/,
      }),
    ).not.toBeInTheDocument();
  });

  it("opens old issue bookmarks in preferences", () => {
    navigationState.search = "tab=issue";
    renderWithI18n(<SettingsPage />);
    expect(screen.getByText("PreferencesTab")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Preferences" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("opens the GitHub App return URL on the Code page", () => {
    navigationState.search = "tab=repositories&github_connected=1";
    renderWithI18n(<SettingsPage />);
    expect(screen.getByText("CodeTab")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Code" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("lists Connected apps with the personal pages only when Composio is available", () => {
    const first = renderWithI18n(<SettingsPage />);
    expect(screen.queryByRole("link", { name: "Connected apps" })).toBeNull();
    first.unmount();

    shell.appsAvailable = true;
    navigationState.search = "tab=integrations&connected=notion";
    renderWithI18n(<SettingsPage />);
    const personal = within(nav()).getByRole("region", { name: /Personal/ });
    expect(
      within(personal).getByRole("link", { name: "Connected apps" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.getByText("ConnectedAppsTab")).toBeInTheDocument();
  });

  it("marks pages a member can only view", () => {
    shell.role = "member";
    renderWithI18n(<SettingsPage />);
    const general = screen.getByRole("link", { name: /^General/ });
    expect(
      within(general).getByLabelText("Only owners and admins can change this"),
    ).toBeInTheDocument();
    // Labels stay editable for every member, so they carry no lock.
    const labels = screen.getByRole("link", { name: "Labels" });
    expect(within(labels).queryByLabelText(/Only owners/)).toBeNull();
  });

  it("places platform settings in a device group", () => {
    renderWithI18n(
      <SettingsPage
        extraDeviceTabs={[
          {
            value: "updates",
            label: "Updates",
            icon: () => null,
            content: <div>Device updates</div>,
          },
        ]}
      />,
    );
    const device = screen.getByRole("region", { name: "This device" });
    expect(
      within(device).getByRole("link", { name: "Updates" }),
    ).toBeInTheDocument();
  });

  it("navigates from the compact selector and clears the old detail", () => {
    navigationState.search = "tab=channels&integration=slack&keep=1";
    renderWithI18n(<SettingsPage />);
    fireEvent.change(
      screen.getByRole("combobox", { name: "Go to settings page" }),
      { target: { value: "preferences" } },
    );
    expect(push).toHaveBeenCalledWith("/acme/settings?tab=preferences&keep=1");
  });
});

describe("SettingsPage search", () => {
  it("finds settings inside pages and opens the one chosen", () => {
    renderWithI18n(<SettingsPage />);
    const input = screen.getByRole("searchbox", { name: "Search settings" });

    fireEvent.change(input, { target: { value: "time zone" } });

    const results = screen.getByRole("listbox", { name: "Search settings" });
    const option = within(results).getByRole("option", { name: /Time zone/ });
    expect(option).toHaveTextContent("Personal › Preferences");

    fireEvent.click(option);
    expect(push).toHaveBeenCalledWith(
      "/acme/settings?tab=preferences&section=timezone",
    );
  });

  it("opens the highlighted result from the keyboard and clears on Escape", () => {
    renderWithI18n(<SettingsPage />);
    const input = screen.getByRole("searchbox", { name: "Search settings" });

    fireEvent.change(input, { target: { value: "slack" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(push).toHaveBeenCalledWith(
      "/acme/settings?tab=channels&integration=slack",
    );

    fireEvent.keyDown(input, { key: "Escape" });
    expect(input).toHaveValue("");
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("does not offer pages hidden by feature flags", () => {
    renderWithI18n(<SettingsPage />);
    fireEvent.change(
      screen.getByRole("searchbox", { name: "Search settings" }),
      { target: { value: "plugins" } },
    );
    expect(screen.getByText("No matching settings")).toBeInTheDocument();
  });
});
