import { act, fireEvent, screen } from "@testing-library/react";
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
const stub = vi.hoisted(
  () => (name: string) => () => ({ [name]: () => <div>{name}</div> }),
);
vi.mock("./account-tab", stub("AccountTab"));
vi.mock("./preferences-tab", stub("PreferencesTab"));
vi.mock("./issue-tab", stub("IssueTab"));
vi.mock("./tokens-tab", stub("TokensTab"));
vi.mock("./workspace-tab", stub("WorkspaceTab"));
vi.mock("./members-tab", stub("MembersTab"));
vi.mock("./repositories-tab", stub("RepositoriesTab"));
vi.mock("./github-tab", stub("GitHubTab"));
vi.mock("./integrations-tab", stub("IntegrationsTab"));
vi.mock("./labs-tab", stub("LabsTab"));
vi.mock("./notifications-tab", stub("NotificationsTab"));
vi.mock("./labels-tab", stub("LabelsTab"));
vi.mock("./properties-tab", stub("PropertiesTab"));
vi.mock("./quick-actions-tab", stub("QuickActionsTab"));
vi.mock("./keyboard-shortcuts-tab", stub("KeyboardShortcutsTab"));
vi.mock("./plugins-tab", stub("PluginsTab"));
vi.mock("./billing-tab", stub("BillingTab"));

const replace = vi.fn();
const navigationState = { search: "" };
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    searchParams: new URLSearchParams(navigationState.search),
    pathname: "/acme/settings",
    replace,
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
  return screen.getByRole("button", { name: "Toggle Sidebar" });
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
      screen.queryByRole("button", { name: "Toggle Sidebar" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Settings")).toBeInTheDocument();
  });
});

describe("SettingsPage Plugin feature flag", () => {
  it("hides Plugins and falls back from a direct tab URL when disabled", () => {
    navigationState.search = "tab=plugins";

    renderWithI18n(<SettingsPage />);

    expect(screen.queryByRole("tab", { name: "Plugins" })).not.toBeInTheDocument();
    expect(screen.queryByText("PluginsTab")).not.toBeInTheDocument();
    expect(screen.getByText("AccountTab")).toBeInTheDocument();
  });

  it("shows and mounts Plugins when explicitly enabled", () => {
    navigationState.search = "tab=plugins";
    configStore.getState().setFeatureFlags({ [PLUGINS_V1_FLAG]: true });

    renderWithI18n(<SettingsPage />);

    expect(screen.getByRole("tab", { name: "Plugins" })).toBeInTheDocument();
    expect(screen.getByText("PluginsTab")).toBeInTheDocument();
  });
});

describe("SettingsPage information architecture", () => {
  it("orders account and workspace tabs by their settings groups", () => {
    renderWithI18n(<SettingsPage />);

    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
      "Profile",
      "Preferences",
      "Issue Preferences",
      "Notifications",
      "Shortcuts",
      "API Tokens",
      "Basic Information",
      "Members",
      "Issue Statuses",
      "Labels",
      "Properties",
      "Quick Actions",
      "Repositories",
      "GitHub",
      "Integrations",
      "MCP",
      "Labs",
    ]);
    expect(screen.getByText("Workspace · General")).toBeInTheDocument();
    expect(screen.getByText("Workspace · Issues")).toBeInTheDocument();
    expect(screen.getByText("Workspace · Connections")).toBeInTheDocument();
    expect(screen.getByText("Workspace · Extensions")).toBeInTheDocument();
  });

  it.each([
    [
      "zh-Hans",
      ["工作区 · 常规", "工作区 · 任务", "工作区 · 连接", "工作区 · 扩展"],
    ],
    [
      "ja",
      [
        "ワークスペース · 一般",
        "ワークスペース · タスク",
        "ワークスペース · 接続",
        "ワークスペース · 拡張機能",
      ],
    ],
    [
      "ko",
      ["워크스페이스 · 일반", "워크스페이스 · 태스크", "워크스페이스 · 연결", "워크스페이스 · 확장"],
    ],
  ] as const)("renders localized workspace groups in %s", (locale, labels) => {
    renderWithI18n(<SettingsPage />, { locale });

    for (const label of labels) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("uses the localized plugin name in Chinese", () => {
    configStore.getState().setFeatureFlags({ [PLUGINS_V1_FLAG]: true });

    renderWithI18n(<SettingsPage />, { locale: "zh-Hans" });

    expect(screen.getByRole("tab", { name: "插件" })).toBeInTheDocument();
  });

  it("distinguishes scopes, workspace groups, and nested tabs", () => {
    renderWithI18n(<SettingsPage />);

    const accountHeading = screen.getByText("My Account");
    const subgroupHeading = screen.getByText("Workspace · General");

    expect(accountHeading.className).toContain("text-caption");
    expect(accountHeading.className).toContain("text-muted-foreground");
    expect(subgroupHeading).toHaveClass("text-caption");
    expect(subgroupHeading).toHaveClass("text-muted-foreground");
    expect(accountHeading.parentElement).toHaveClass("md:p-2");
    expect(subgroupHeading.parentElement).toHaveClass("md:p-2");
    expect(screen.getByText("Workspace · Issues").parentElement).toHaveClass(
      "md:p-2",
    );
    expect(screen.queryByText("Workspace")).not.toBeInTheDocument();
    expect(
      screen.getByRole("tab", { name: "Profile" }).parentElement,
    ).toHaveClass("md:gap-0.5");
    expect(
      screen.getByRole("tab", { name: "Basic Information" }).parentElement,
    ).toHaveClass("md:gap-0.5");
    expect(screen.getByRole("tab", { name: "Profile" }).className).toContain(
      "group-data-[variant=line]/tabs-list:data-active:bg-surface-selected",
    );
    expect(screen.getByRole("tab", { name: "Profile" }).className).not.toContain(
      "!",
    );
    expect(screen.getByRole("tab", { name: "Profile" }).className).not.toContain(
      "brand",
    );
  });

  it("keeps keyboard navigation continuous across visual groups", async () => {
    layout.compact = false;
    navigationState.search = "tab=tokens";
    renderWithI18n(<SettingsPage />);

    const lastAccountTab = screen.getByRole("tab", { name: "API Tokens" });
    lastAccountTab.focus();
    await act(async () => {
      fireEvent.keyDown(lastAccountTab, { key: "ArrowRight" });
    });

    expect(
      screen.getByRole("tab", { name: "Basic Information" }),
    ).toHaveFocus();
  });

  it("keeps optional admin tabs in their groups and Labs last", () => {
    configStore.getState().setFeatureFlags({
      [BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG]: true,
      [PLUGINS_V1_FLAG]: true,
    });

    renderWithI18n(<SettingsPage />);

    const labels = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(labels.indexOf("Members")).toBeLessThan(labels.indexOf("Billing"));
    expect(labels.indexOf("Plugins")).toBeLessThan(labels.indexOf("MCP"));
    expect(labels.at(-1)).toBe("Labs");
  });

  it("renders platform settings in their own group between account and workspace", () => {
    renderWithI18n(
      <SettingsPage
        extraSettingsGroups={[
          {
            label: "Desktop",
            tabs: [
              {
                value: "daemon",
                label: "Daemon",
                icon: () => null,
                content: <div>DaemonTab</div>,
              },
            ],
          },
        ]}
      />,
    );

    const labels = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(labels.indexOf("API Tokens")).toBeLessThan(labels.indexOf("Daemon"));
    expect(labels.indexOf("Daemon")).toBeLessThan(
      labels.indexOf("Basic Information"),
    );
    expect(screen.getByText("Desktop")).toHaveClass("h-8");
    expect(screen.getByText("Desktop").parentElement).toHaveClass("md:p-2");
    expect(screen.getByText("Workspace · General")).toHaveClass("h-8");
    expect(
      screen.getByText("Workspace · General").parentElement,
    ).toHaveClass("md:p-2");
  });

  it("keeps old Chat settings links on the merged Preferences surface", () => {
    navigationState.search = "tab=chat";

    renderWithI18n(<SettingsPage />);

    expect(screen.queryByRole("tab", { name: "Chat" })).not.toBeInTheDocument();
    expect(screen.getByText("PreferencesTab")).toBeInTheDocument();
  });
});

describe("SettingsPage workspace subscription feature flag", () => {
  it("hides Billing and falls back to Workspace General from a direct URL", () => {
    navigationState.search = "tab=billing";

    renderWithI18n(<SettingsPage />);

    expect(
      screen.queryByRole("tab", { name: "Billing" }),
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

    expect(screen.getByRole("tab", { name: "Billing" })).toBeInTheDocument();
    expect(screen.getByText("BillingTab")).toBeInTheDocument();
  });
});
