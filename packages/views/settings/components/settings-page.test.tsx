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
vi.mock("./chat-tab", stub("ChatTab"));
vi.mock("./issue-tab", stub("IssueTab"));
vi.mock("./tokens-tab", stub("TokensTab"));
vi.mock("./workspace-tab", stub("WorkspaceTab"));
vi.mock("./members-tab", stub("MembersTab"));
vi.mock("./repositories-tab", stub("RepositoriesTab"));
vi.mock("./github-tab", stub("GitHubTab"));
vi.mock("./integrations-tab", stub("IntegrationsTab"));
vi.mock("./notifications-tab", stub("NotificationsTab"));
vi.mock("./labels-tab", stub("LabelsTab"));
vi.mock("./properties-tab", stub("PropertiesTab"));
vi.mock("./quick-actions-tab", stub("QuickActionsTab"));
vi.mock("./keyboard-shortcuts-tab", stub("KeyboardShortcutsTab"));
vi.mock("./plugins-tab", stub("PluginsTab"));
vi.mock("./billing-tab", stub("BillingTab"));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Acme" }),
}));

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

beforeEach(() => {
  layout.compact = true;
  navigationState.search = "";
  configStore.getState().setFeatureFlags({});
  replace.mockClear();
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
    expect(screen.getByText("Settings")).toBeInTheDocument();
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
  it("groups workspace configuration and keeps retired pages out of navigation", () => {
    renderWithI18n(<SettingsPage />);
    const nav = screen.getByRole("navigation", { name: "Settings" });
    const issues = within(nav).getByRole("region", {
      name: "Issue configuration",
    });
    expect(
      within(issues).getByRole("link", { name: "Issue Statuses" }),
    ).toBeInTheDocument();
    expect(
      within(nav).queryByRole("link", { name: /^(Issue|Chat|GitHub|Labs)$/ }),
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
    const device = screen.getByRole("region", { name: "Desktop app" });
    expect(
      within(device).getByRole("link", { name: "Updates" }),
    ).toBeInTheDocument();
  });

  it("navigates from the compact selector and clears the old detail", () => {
    navigationState.search = "tab=integrations&integration=github&keep=1";
    renderWithI18n(<SettingsPage />);
    fireEvent.change(
      screen.getByRole("combobox", { name: "Go to settings page" }),
      { target: { value: "preferences" } },
    );
    expect(push).toHaveBeenCalledWith("/acme/settings?tab=preferences&keep=1");
  });
});
