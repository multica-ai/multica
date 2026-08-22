"use client";

import React from "react";
import {
  User,
  SlidersHorizontal,
  Key,
  Settings,
  Users,
  FolderGit2,
  FlaskConical,
  Bell,
  Plug,
  Tags,
  CircleDot,
  Keyboard,
  ListTodo,
  Zap,
  Blocks,
  CreditCard,
  Server,
} from "lucide-react";
import { GitHubMark } from "./github-mark";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
} from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useFeatureEnabled } from "@multica/core/config";
import {
  BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
  PLUGINS_V1_FLAG,
} from "@multica/core/feature-flags";
import { useNavigation } from "../../navigation";
import { AccountTab } from "./account-tab";
import { PreferencesTab } from "./preferences-tab";
import { IssueTab } from "./issue-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { GitHubTab } from "./github-tab";
import { IntegrationsTab } from "./integrations-tab";
import { LabsTab } from "./labs-tab";
import { NotificationsTab } from "./notifications-tab";
import { LabelsTab } from "./labels-tab";
import { IssueStatusesTab } from "./issue-statuses-tab";
import { PropertiesTab } from "./properties-tab";
import { QuickActionsTab } from "./quick-actions-tab";
import { KeyboardShortcutsTab } from "./keyboard-shortcuts-tab";
import { PluginsTab } from "./plugins-tab";
import { McpTab } from "./mcp-tab";
import { BillingTab } from "./billing-tab";
import { CollapsedNavTrigger } from "../../layout/page-header";
import { useT } from "../../i18n";

const ACCOUNT_TAB_KEYS = [
  "profile",
  "preferences",
  "issue",
  "notifications",
  "shortcuts",
  "tokens",
] as const;
const ACCOUNT_TAB_ICONS = {
  profile: User,
  preferences: SlidersHorizontal,
  issue: ListTodo,
  notifications: Bell,
  shortcuts: Keyboard,
  tokens: Key,
} as const;

const WORKSPACE_TAB_VALUES = {
  general: "workspace",
  repositories: "repositories",
  github: "github",
  integrations: "integrations",
  labs: "labs",
  members: "members",
  billing: "billing",
  labels: "labels",
  issue_statuses: "issue-statuses",
  properties: "properties",
  quick_actions: "quick-actions",
  mcp: "mcp",
  plugins: "plugins",
} as const;
type WorkspaceTabKey = keyof typeof WORKSPACE_TAB_VALUES;

const WORKSPACE_TAB_GROUPS: ReadonlyArray<{
  label: "management" | "tasks" | "connections" | "extensions";
  tabs: readonly WorkspaceTabKey[];
}> = [
  {
    label: "management",
    tabs: ["general", "members", "billing"],
  },
  {
    label: "tasks",
    tabs: ["issue_statuses", "labels", "properties", "quick_actions"],
  },
  {
    label: "connections",
    tabs: ["repositories", "github", "integrations"],
  },
  {
    label: "extensions",
    tabs: ["plugins", "mcp", "labs"],
  },
];

const WORKSPACE_TAB_ICONS = {
  general: Settings,
  repositories: FolderGit2,
  github: GitHubMark,
  integrations: Plug,
  labs: FlaskConical,
  members: Users,
  billing: CreditCard,
  labels: Tags,
  issue_statuses: CircleDot,
  properties: SlidersHorizontal,
  quick_actions: Zap,
  mcp: Server,
  plugins: Blocks,
} as const;

const DEFAULT_TAB = "profile";
const TAB_QUERY_KEY = "tab";

// Legacy `?tab=…` values that have been collapsed into another tab. Old
// bookmarks still land on the correct surface without us preserving dead
// TabsContent entries.
const LEGACY_TAB_REDIRECTS: Record<string, string> = {
  lark: "integrations",
  chat: "preferences",
};

const SETTINGS_TAB_TRIGGER_CLASS = cn(
  "h-8 shrink-0 px-2.5 font-normal hover:bg-surface-hover",
  "data-active:font-medium data-active:text-surface-selected-foreground",
  "group-data-[variant=line]/tabs-list:data-active:bg-surface-selected group-data-[variant=line]/tabs-list:data-active:hover:bg-surface-selected",
  "dark:data-active:text-surface-selected-foreground dark:group-data-[variant=line]/tabs-list:data-active:bg-surface-selected dark:group-data-[variant=line]/tabs-list:data-active:hover:bg-surface-selected",
  "md:px-2 md:after:hidden",
);

const SETTINGS_NAV_GROUP_ITEMS_CLASS =
  "flex w-auto min-w-0 flex-row gap-1 md:w-full md:flex-col md:gap-0.5";

function SettingsNavGroup({
  label,
  children,
}: {
  label: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <SidebarGroup className="w-auto flex-row p-0 md:w-full md:flex-col md:p-2">
      <SidebarGroupLabel className="hidden min-w-0 truncate md:flex">
        {label}
      </SidebarGroupLabel>
      <SidebarGroupContent className="w-auto md:w-full">
        <div className={SETTINGS_NAV_GROUP_ITEMS_CLASS}>{children}</div>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

export interface ExtraSettingsGroup {
  label: string;
  tabs: ExtraSettingsTab[];
}

interface SettingsPageProps {
  /** Additional platform-scoped groups (e.g. settings for the desktop app). */
  extraSettingsGroups?: ExtraSettingsGroup[];
}

export function SettingsPage({ extraSettingsGroups }: SettingsPageProps = {}) {
  const { t } = useT("settings");
  const navigation = useNavigation();
  const isMobile = useIsMobile();
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );

  const visibleWorkspaceTabGroups = React.useMemo(
    () =>
      WORKSPACE_TAB_GROUPS.map((group) => ({
        ...group,
        tabs: group.tabs.filter(
          (key) =>
            (key !== "plugins" || pluginsEnabled) &&
            (key !== "billing" || billingEnabled),
        ),
      })),
    [billingEnabled, pluginsEnabled],
  );
  const visibleWorkspaceTabKeys = React.useMemo(
    () => visibleWorkspaceTabGroups.flatMap((group) => group.tabs),
    [visibleWorkspaceTabGroups],
  );

  // Whitelist of valid tab values; unknown ?tab=… values silently fall back to
  // the default. Whitelisting also blocks junk like ?tab=<script> from
  // surfacing in the DOM via Radix Tabs internals.
  const validTabs = React.useMemo(
    () =>
      new Set<string>([
        ...ACCOUNT_TAB_KEYS,
        ...visibleWorkspaceTabKeys.map((key) => WORKSPACE_TAB_VALUES[key]),
        ...(extraSettingsGroups?.flatMap((group) =>
          group.tabs.map((tab) => tab.value),
        ) ?? []),
      ]),
    [extraSettingsGroups, visibleWorkspaceTabKeys],
  );

  const tabFromUrl = navigation.searchParams.get(TAB_QUERY_KEY);
  const candidateTab = tabFromUrl
    ? tabFromUrl === "billing" && !billingEnabled
      ? "workspace"
      : LEGACY_TAB_REDIRECTS[tabFromUrl] ?? tabFromUrl
    : null;
  const activeTab =
    candidateTab && validTabs.has(candidateTab) ? candidateTab : DEFAULT_TAB;

  // replace (not push) so settings tab switches don't pollute browser history.
  // Preserve any other query params the page may carry.
  const handleTabChange = (next: string) => {
    const params = new URLSearchParams(navigation.searchParams);
    params.set(TAB_QUERY_KEY, next);
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
  };

  return (
    <Tabs
      value={activeTab}
      onValueChange={handleTabChange}
      orientation={isMobile ? "horizontal" : "vertical"}
      className="flex flex-1 min-h-0 flex-col gap-0 overflow-y-auto md:flex-row md:overflow-hidden"
    >
      {/* Structural navigation; bounded setting groups remain in the content surface.
          Stays on the content surface color (no shell tint): the desktop's active
          tab merges into the card top, and a tinted panel under the first tabs
          breaks that seam (MUL-4439). Zoning comes from the divider instead. */}
      <div className="shrink-0 overflow-x-auto border-b border-surface-border p-2 md:w-56 md:overflow-y-auto md:border-b-0 md:border-r md:p-4">
        {/* This page builds its own chrome instead of a PageHeader, so it has
            to supply the nav trigger itself — below `xl` the nav is a sheet or
            auto-collapsed, and settings has no other way back to it. */}
        {/* The gap below this row belongs to the row, not to the heading: with
            `items-center`, a bottom margin on the `h1` is part of the box being
            centred, so it offsets the heading against the trigger beside it. */}
        <div className="flex items-center md:mb-2.5">
          <CollapsedNavTrigger />
          <h1 className="sr-only text-title-sm font-semibold md:not-sr-only md:px-2.5">
            {t(($) => $.page.title)}
          </h1>
        </div>
        <TabsList
          variant="line"
          className="flex w-max min-w-full flex-row items-center gap-1 p-0 md:w-full md:flex-col md:items-stretch md:gap-0"
        >
          {/* My Account group */}
          <SettingsNavGroup label={t(($) => $.page.my_account)}>
            {ACCOUNT_TAB_KEYS.map((key) => {
              const Icon = ACCOUNT_TAB_ICONS[key];
              return (
                <TabsTrigger
                  key={key}
                  value={key}
                  className={SETTINGS_TAB_TRIGGER_CLASS}
                >
                  <Icon className="h-4 w-4" />
                  {t(($) => $.page.tabs[key])}
                </TabsTrigger>
              );
            })}
          </SettingsNavGroup>
          {extraSettingsGroups?.map((group) => (
            <SettingsNavGroup key={group.label} label={group.label}>
              {group.tabs.map((tab) => (
                <TabsTrigger
                  key={tab.value}
                  value={tab.value}
                  className={SETTINGS_TAB_TRIGGER_CLASS}
                >
                  <tab.icon className="h-4 w-4" />
                  {tab.label}
                </TabsTrigger>
              ))}
            </SettingsNavGroup>
          ))}

          {/* Workspace group */}
          {visibleWorkspaceTabGroups.map((group) => (
            <SettingsNavGroup
              key={group.label}
              label={t(($) => $.page.groups[group.label])}
            >
              {group.tabs.map((key) => {
                const Icon = WORKSPACE_TAB_ICONS[key];
                return (
                  <TabsTrigger
                    key={key}
                    value={WORKSPACE_TAB_VALUES[key]}
                    className={SETTINGS_TAB_TRIGGER_CLASS}
                  >
                    <Icon className="h-4 w-4" />
                    {t(($) => $.page.tabs[key])}
                  </TabsTrigger>
                );
              })}
            </SettingsNavGroup>
          ))}
        </TabsList>
      </div>

      {/* Right content */}
      <div className="min-w-0 flex-1 md:overflow-y-auto">
        <div className={`mx-auto w-full p-4 sm:p-6 md:p-8 ${activeTab === "labels" || activeTab === "issue-statuses" || activeTab === "properties" || activeTab === "quick-actions"
              ? "max-w-5xl"
              : "max-w-3xl"}`}>
          <TabsContent value="profile"><AccountTab /></TabsContent>
          <TabsContent value="preferences"><PreferencesTab /></TabsContent>
          <TabsContent value="issue"><IssueTab /></TabsContent>
          <TabsContent value="notifications"><NotificationsTab /></TabsContent>
          <TabsContent value="shortcuts"><KeyboardShortcutsTab /></TabsContent>
          <TabsContent value="tokens"><TokensTab /></TabsContent>
          <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
          <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
          <TabsContent value="github"><GitHubTab /></TabsContent>
          <TabsContent value="integrations"><IntegrationsTab /></TabsContent>
          <TabsContent value="labs"><LabsTab /></TabsContent>
          <TabsContent value="members"><MembersTab /></TabsContent>
          {billingEnabled ? (
            <TabsContent value="billing"><BillingTab /></TabsContent>
          ) : null}
          <TabsContent value="labels"><LabelsTab /></TabsContent>
          <TabsContent value="issue-statuses"><IssueStatusesTab /></TabsContent>
          <TabsContent value="properties"><PropertiesTab /></TabsContent>
          <TabsContent value="quick-actions"><QuickActionsTab /></TabsContent>
          <TabsContent value="mcp"><McpTab /></TabsContent>
          {pluginsEnabled ? <TabsContent value="plugins"><PluginsTab /></TabsContent> : null}
          {extraSettingsGroups?.flatMap((group) =>
            group.tabs.map((tab) => (
              <TabsContent key={tab.value} value={tab.value}>
                {tab.content}
              </TabsContent>
            )),
          )}
        </div>
      </div>
    </Tabs>
  );
}
