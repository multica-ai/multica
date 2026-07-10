"use client";

import React, { useEffect, useMemo, useRef } from "react";
import {
  Bell,
  FolderGit2,
  Key,
  Layers3,
  Plug,
  Tags,
  Keyboard,
  Settings,
  SlidersHorizontal,
  User,
  Users,
  Workflow,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../../navigation";
import { AccountTab } from "./account-tab";
import { PreferencesTab } from "./preferences-tab";
import { ChatTab } from "./chat-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { IntegrationsTab } from "./integrations-tab";
import { NotificationsTab } from "./notifications-tab";
import { LabelsTab } from "./labels-tab";
import { PropertiesTab } from "./properties-tab";
import { KeyboardShortcutsTab } from "./keyboard-shortcuts-tab";
import { WorkspaceSpacesTab } from "./workspace-spaces-tab";
import { AutopilotTemplatesTab } from "./autopilot-templates-tab";
import { useT } from "../../i18n";

type SettingsScope = "account" | "workspace" | "device";

interface SettingsDestination {
  scope: SettingsScope;
  key: string;
  label: string;
  icon: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
  content: React.ReactNode;
}

const LEGACY_TAB_PATHS: Record<string, string> = {
  profile: "account/profile",
  preferences: "account/preferences",
  shortcuts: "account/shortcuts",
  chat: "account/preferences",
  notifications: "account/notifications",
  tokens: "account/tokens",
  workspace: "workspace/general",
  general: "workspace/general",
  members: "workspace/members",
  repositories: "workspace/repositories",
  github: "workspace/integrations",
  integrations: "workspace/integrations",
  lark: "workspace/integrations",
  labs: "workspace/general",
  labels: "workspace/labels",
  properties: "workspace/properties",
};

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
  content: React.ReactNode;
}

interface SettingsPageProps {
  /** Device-scoped pages injected by a platform, such as desktop updates. */
  extraDeviceTabs?: ExtraSettingsTab[];
}

function settingsSuffix(pathname: string): string | null {
  const marker = "/settings/";
  const markerIndex = pathname.indexOf(marker);
  if (markerIndex === -1) return null;
  return pathname.slice(markerIndex + marker.length).replace(/\/$/, "");
}

export function SettingsPage({ extraDeviceTabs }: SettingsPageProps = {}) {
  const { t } = useT("settings");
  const workspaceName = useCurrentWorkspace()?.name;
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  const groups = useMemo(() => {
    const account: SettingsDestination[] = [
      {
        scope: "account",
        key: "profile",
        label: t(($) => $.page.tabs.profile),
        icon: User,
        content: <AccountTab />,
      },
      {
        scope: "account",
        key: "preferences",
        label: t(($) => $.page.tabs.preferences),
        icon: SlidersHorizontal,
        content: (
          <div className="space-y-10">
            <PreferencesTab />
            <ChatTab />
          </div>
        ),
      },
      {
        scope: "account",
        key: "notifications",
        label: t(($) => $.page.tabs.notifications),
        icon: Bell,
        content: <NotificationsTab />,
      },
      {
        scope: "account",
        key: "shortcuts",
        label: t(($) => $.page.tabs.shortcuts),
        icon: Keyboard,
        content: <KeyboardShortcutsTab />,
      },
      {
        scope: "account",
        key: "tokens",
        label: t(($) => $.page.tabs.tokens),
        icon: Key,
        content: <TokensTab />,
      },
    ];
    const workspace: SettingsDestination[] = [
      {
        scope: "workspace",
        key: "general",
        label: t(($) => $.page.tabs.general),
        icon: Settings,
        content: <WorkspaceTab />,
      },
      {
        scope: "workspace",
        key: "members",
        label: t(($) => $.page.tabs.members),
        icon: Users,
        content: <MembersTab />,
      },
      {
        scope: "workspace",
        key: "spaces",
        label: t(($) => $.page.tabs.spaces),
        icon: Layers3,
        content: <WorkspaceSpacesTab />,
      },
      {
        scope: "workspace",
        key: "labels",
        label: t(($) => $.page.tabs.labels),
        icon: Tags,
        content: <LabelsTab />,
      },
      {
        scope: "workspace",
        key: "properties",
        label: t(($) => $.page.tabs.properties),
        icon: SlidersHorizontal,
        content: <PropertiesTab />,
      },
      {
        scope: "workspace",
        key: "autopilot-templates",
        label: t(($) => $.page.tabs.autopilot_templates),
        icon: Workflow,
        content: <AutopilotTemplatesTab />,
      },
      {
        scope: "workspace",
        key: "integrations",
        label: t(($) => $.page.tabs.integrations),
        icon: Plug,
        content: <IntegrationsTab />,
      },
      {
        scope: "workspace",
        key: "repositories",
        label: t(($) => $.page.tabs.repositories),
        icon: FolderGit2,
        content: <RepositoriesTab />,
      },
    ];
    const device: SettingsDestination[] = (extraDeviceTabs ?? []).map((tab) => ({
      scope: "device",
      key: tab.value,
      label: tab.label,
      icon: tab.icon,
      content: tab.content,
    }));
    return { account, workspace, device };
  }, [extraDeviceTabs, t]);

  const destinations = useMemo(
    () => [...groups.account, ...groups.workspace, ...groups.device],
    [groups],
  );
  const destinationByPath = useMemo(
    () =>
      new Map(
        destinations.map((destination) => [
          `${destination.scope}/${destination.key}`,
          destination,
        ]),
      ),
    [destinations],
  );

  const suffix = settingsSuffix(navigation.pathname);
  const legacyTab = navigation.searchParams.get("tab");
  const requestedPath = suffix ?? (legacyTab ? LEGACY_TAB_PATHS[legacyTab] : null);
  const active =
    (requestedPath ? destinationByPath.get(requestedPath) : null) ?? groups.account[0]!;
  const activePath = `${active.scope}/${active.key}`;
  const canonicalPath =
    suffix === activePath && legacyTab === null
      ? null
      : paths.settingsSection(active.scope, active.key);
  const lastRequestedCanonicalPath = useRef<string | null>(null);

  // Canonicalise old query-tab bookmarks and unknown/root Settings URLs. This
  // keeps one stable URL per page while preserving every legacy entry point.
  // The navigation adapter may re-render while a route transition is pending;
  // remember the in-flight target so that render cannot enqueue the same
  // replace repeatedly before the pathname commits.
  useEffect(() => {
    if (canonicalPath === null) {
      lastRequestedCanonicalPath.current = null;
      return;
    }
    if (lastRequestedCanonicalPath.current === canonicalPath) return;
    lastRequestedCanonicalPath.current = canonicalPath;
    navigation.replace(canonicalPath);
  }, [canonicalPath, navigation]);

  const selectDestination = (path: string | null) => {
    if (!path) return;
    const destination = destinationByPath.get(path);
    if (!destination) return;
    navigation.push(paths.settingsSection(destination.scope, destination.key));
  };

  const groupEntries: Array<{
    scope: SettingsScope;
    label: string;
    entries: SettingsDestination[];
  }> = [
    {
      scope: "account",
      label: t(($) => $.page.my_account),
      entries: groups.account,
    },
    {
      scope: "workspace",
      label: workspaceName ?? t(($) => $.page.workspace_fallback),
      entries: groups.workspace,
    },
    ...(groups.device.length > 0
      ? [
          {
            scope: "device" as const,
            label: t(($) => $.page.this_device),
            entries: groups.device,
          },
        ]
      : []),
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row md:overflow-hidden">
      <div className="border-b border-surface-border bg-app-shell/70 p-3 md:hidden">
        <Select value={activePath} onValueChange={selectDestination}>
          <SelectTrigger className="w-full" aria-label={t(($) => $.page.title)}>
            <SelectValue>{active.label}</SelectValue>
          </SelectTrigger>
          <SelectContent align="start">
            {groupEntries.map((group) => (
              <SelectGroup key={group.scope}>
                <SelectLabel>{group.label}</SelectLabel>
                {group.entries.map((entry) => {
                  const Icon = entry.icon;
                  return (
                    <SelectItem
                      key={`${entry.scope}/${entry.key}`}
                      value={`${entry.scope}/${entry.key}`}
                    >
                      <Icon className="size-4" aria-hidden />
                      {entry.label}
                    </SelectItem>
                  );
                })}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
      </div>

      <nav
        aria-label={t(($) => $.page.title)}
        className="hidden w-56 shrink-0 overflow-y-auto border-r border-surface-border bg-app-shell/70 p-4 md:block"
      >
        <h1 className="mb-4 px-2 text-sm font-semibold">
          {t(($) => $.page.title)}
        </h1>
        <div className="space-y-4">
          {groupEntries.map((group) => (
            <section key={group.scope} aria-labelledby={`settings-${group.scope}`}>
              <h2
                id={`settings-${group.scope}`}
                className="mb-1 truncate px-2 text-xs font-medium text-muted-foreground"
              >
                {group.label}
              </h2>
              <div className="space-y-0.5">
                {group.entries.map((entry) => {
                  const path = `${entry.scope}/${entry.key}`;
                  const Icon = entry.icon;
                  return (
                    <AppLink
                      key={path}
                      href={paths.settingsSection(entry.scope, entry.key)}
                      aria-current={path === activePath ? "page" : undefined}
                      className={cn(
                        "flex h-8 items-center gap-2 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                        path === activePath &&
                          "bg-surface-selected font-medium text-surface-selected-foreground hover:bg-surface-selected",
                      )}
                    >
                      <Icon className="size-4" aria-hidden />
                      <span className="truncate">{entry.label}</span>
                    </AppLink>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      </nav>

      <main className="min-w-0 flex-1 overflow-y-auto">
        <div
          className={cn(
            "mx-auto w-full p-4 md:p-6",
            active.key === "labels" || active.key === "properties"
              ? "max-w-5xl"
              : "max-w-3xl",
          )}
        >
          {active.content}
        </div>
      </main>
    </div>
  );
}
