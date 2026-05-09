"use client";

// CEREBRO-PATCH(settings-page-cerebro): cerebro modification of upstream file

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
  Sparkles,
  BookText,
} from "lucide-react";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@multica/ui/components/ui/tabs";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { AccountTab } from "./account-tab";
import { PreferencesTab } from "./preferences-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { LabsTab } from "./labs-tab";
// CEREBRO-PATCH(settings-page-notifications): use cerebro notifications-tab (Phase 1b relocation + push UI)
import { NotificationsTab } from "@multica/cerebro-notifications/views/notifications-tab";
// CEREBRO-PATCH(settings-page-agent-profile): cerebro agent profile tab
import { AgentProfileTab } from "@multica/cerebro-profile/views";
import { useT } from "../../i18n";
import {
  CerebroMobileTabNav,
  type CerebroMobileTabNavGroup,
} from "./cerebro-mobile-tab-nav";

const ACCOUNT_TAB_KEYS = ["profile", "preferences", "notifications", "tokens"] as const;
const ACCOUNT_TAB_ICONS = {
  profile: User,
  preferences: SlidersHorizontal,
  notifications: Bell,
  tokens: Key,
} as const;
// CEREBRO-PATCH(settings-page-agent-profile-key): cerebro Agent Profile tab value
const AGENT_PROFILE_TAB_VALUE = "agent-profile";

const WORKSPACE_TAB_KEYS = ["general", "repositories", "labs", "members"] as const;
const WORKSPACE_TAB_VALUES = {
  general: "workspace",
  repositories: "repositories",
  labs: "labs",
  members: "members",
} as const;
const WORKSPACE_TAB_ICONS = {
  general: Settings,
  repositories: FolderGit2,
  labs: FlaskConical,
  members: Users,
} as const;

const DEFAULT_TAB = "profile";
const TAB_QUERY_KEY = "tab";

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

interface SettingsPageProps {
  /** Additional tabs injected by platform (e.g. desktop daemon settings) */
  extraAccountTabs?: ExtraSettingsTab[];
  /**
   * Content rendered inside the Documentation tab. The platform layer is
   * responsible for loading MDX/source data; SettingsPage only frames it.
   * When omitted, the Documentation tab is hidden entirely.
   */
  documentationContent?: React.ReactNode;
}

export function SettingsPage({
  extraAccountTabs,
  documentationContent,
}: SettingsPageProps = {}) {
  const { t } = useT("settings");
  const workspaceName = useCurrentWorkspace()?.name;
  const navigation = useNavigation();

  // Whitelist of valid tab values; unknown ?tab=… values silently fall back to
  // the default. Whitelisting also blocks junk like ?tab=<script> from
  // surfacing in the DOM via Radix Tabs internals.
  const validTabs = React.useMemo(
    () =>
      new Set<string>([
        ...ACCOUNT_TAB_KEYS,
        // CEREBRO-PATCH(settings-page-agent-profile-valid): include cerebro Agent Profile tab
        AGENT_PROFILE_TAB_VALUE,
        ...Object.values(WORKSPACE_TAB_VALUES),
        // CEREBRO-PATCH(settings-page-documentation): documentation tab is valid
        ...(documentationContent ? ["documentation"] : []),
        ...(extraAccountTabs?.map((tab) => tab.value) ?? []),
      ]),
    [extraAccountTabs, documentationContent],
  );

  const tabFromUrl = navigation.searchParams.get(TAB_QUERY_KEY);
  const activeTab =
    tabFromUrl && validTabs.has(tabFromUrl) ? tabFromUrl : DEFAULT_TAB;

  // replace (not push) so settings tab switches don't pollute browser history.
  // Preserve any other query params the page may carry.
  const handleTabChange = (next: string) => {
    const params = new URLSearchParams(navigation.searchParams);
    params.set(TAB_QUERY_KEY, next);
    navigation.replace(`${navigation.pathname}?${params.toString()}`);
  };

  // CEREBRO-PATCH(settings-mobile-nav): collapse the long vertical tablist to
  // a mobile Select so settings content is visible without scrolling past the
  // entire inner sidebar first.
  const mobileTabGroups = React.useMemo<CerebroMobileTabNavGroup[]>(() => {
    const accountItems: CerebroMobileTabNavGroup["items"] = ACCOUNT_TAB_KEYS.map((key) => ({
      value: key,
      label: t(($) => $.page.tabs[key]),
      icon: ACCOUNT_TAB_ICONS[key],
    }));
    accountItems.push({
      value: AGENT_PROFILE_TAB_VALUE,
      label: "Agent Profile",
      icon: Sparkles,
    });
    extraAccountTabs?.forEach((tab) => {
      accountItems.push({
        value: tab.value,
        label: tab.label,
        icon: tab.icon,
      });
    });

    const groups: CerebroMobileTabNavGroup[] = [
      {
        label: t(($) => $.page.my_account),
        items: accountItems,
      },
      {
        label: workspaceName ?? t(($) => $.page.workspace_fallback),
        items: WORKSPACE_TAB_KEYS.map((key) => ({
          value: WORKSPACE_TAB_VALUES[key],
          label: t(($) => $.page.tabs[key]),
          icon: WORKSPACE_TAB_ICONS[key],
        })),
      },
    ];

    if (documentationContent) {
      groups.push({
        label: "Resources",
        items: [
          {
            value: "documentation",
            label: "Documentation",
            icon: BookText,
          },
        ],
      });
    }

    return groups;
  }, [documentationContent, extraAccountTabs, t, workspaceName]);

  return (
    <Tabs
      value={activeTab}
      onValueChange={handleTabChange}
      orientation="vertical"
      className="flex-1 min-h-0 gap-0 flex flex-col md:flex-row md:overflow-hidden overflow-y-auto"
    >
      <CerebroMobileTabNav
        value={activeTab}
        onValueChange={handleTabChange}
        groups={mobileTabGroups}
      />
      {/* Left nav (stacks on top on mobile, sidebar on md+) */}
      <div className="hidden shrink-0 border-b p-3 md:flex md:w-52 md:flex-col md:border-b-0 md:border-r md:overflow-y-auto md:p-4">
        <h1 className="text-sm font-semibold mb-4 px-2">{t(($) => $.page.title)}</h1>
        <TabsList variant="line" className="flex-col items-stretch w-full">
          {/* My Account group */}
          <span className="px-2 pb-1 pt-2 text-xs font-medium text-muted-foreground">
            {t(($) => $.page.my_account)}
          </span>
          {ACCOUNT_TAB_KEYS.map((key) => {
            const Icon = ACCOUNT_TAB_ICONS[key];
            return (
              <TabsTrigger key={key} value={key}>
                <Icon className="h-4 w-4" />
                {t(($) => $.page.tabs[key])}
              </TabsTrigger>
            );
          })}
          {/* CEREBRO-PATCH(settings-page-agent-profile-trigger): cerebro Agent Profile tab trigger */}
          <TabsTrigger value={AGENT_PROFILE_TAB_VALUE}>
            <Sparkles className="h-4 w-4" />
            Agent Profile
          </TabsTrigger>
          {extraAccountTabs?.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <tab.icon className="h-4 w-4" />
              {tab.label}
            </TabsTrigger>
          ))}

          {/* Workspace group */}
          <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground truncate">
            {workspaceName ?? t(($) => $.page.workspace_fallback)}
          </span>
          {WORKSPACE_TAB_KEYS.map((key) => {
            const Icon = WORKSPACE_TAB_ICONS[key];
            return (
              <TabsTrigger key={key} value={WORKSPACE_TAB_VALUES[key]}>
                <Icon className="h-4 w-4" />
                {t(($) => $.page.tabs[key])}
              </TabsTrigger>
            );
          })}

          {/* CEREBRO-PATCH(settings-page-documentation): Resources group with Documentation tab */}
          {documentationContent && (
            <>
              <span className="px-2 pb-1 pt-4 text-xs font-medium text-muted-foreground">
                Resources
              </span>
              <TabsTrigger value="documentation">
                <BookText className="h-4 w-4" />
                Documentation
              </TabsTrigger>
            </>
          )}
        </TabsList>
      </div>

      {/* Right content. Documentation gets the full pane (it brings its own
          internal layout); other tabs share a constrained, scrollable container. */}
      <div className="flex-1 min-w-0 flex flex-col min-h-0 md:overflow-y-auto">
        {activeTab === "documentation" && documentationContent ? (
          <div className="flex-1 min-h-0 relative">{documentationContent}</div>
        ) : (
          <div className="w-full max-w-3xl mx-auto p-4 md:p-6">
            <TabsContent value="profile"><AccountTab /></TabsContent>
            {/* CEREBRO-PATCH(settings-page-agent-profile-content): agent profile tab content */}
            <TabsContent value="agent-profile"><AgentProfileTab /></TabsContent>
            <TabsContent value="preferences"><PreferencesTab /></TabsContent>
            <TabsContent value="notifications"><NotificationsTab /></TabsContent>
            <TabsContent value="tokens"><TokensTab /></TabsContent>
            <TabsContent value="workspace"><WorkspaceTab /></TabsContent>
            <TabsContent value="repositories"><RepositoriesTab /></TabsContent>
            <TabsContent value="labs"><LabsTab /></TabsContent>
            <TabsContent value="members"><MembersTab /></TabsContent>
            {extraAccountTabs?.map((tab) => (
              <TabsContent key={tab.value} value={tab.value}>{tab.content}</TabsContent>
            ))}
          </div>
        )}
      </div>
    </Tabs>
  );
}
