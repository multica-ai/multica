"use client";

import React from "react";
import {
  ChevronRight,
  User,
  SlidersHorizontal,
  Key,
  Settings,
  Users,
  FolderGit2,
  Bell,
  Plug,
  Tags,
  CircleDot,
  Keyboard,
  Zap,
  Blocks,
  CreditCard,
  Server,
} from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureEnabled } from "@multica/core/config";
import {
  BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
  PLUGINS_V1_FLAG,
} from "@multica/core/feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { resolveSettingsLocation, settingsHref } from "./settings-navigation";
import { AppLink, useNavigation } from "../../navigation";
import { AccountTab } from "./account-tab";
import { PreferencesTab } from "./preferences-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { RepositoriesTab } from "./repositories-tab";
import { IntegrationsTab } from "./integrations-tab";
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

export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

interface SettingsPageProps {
  /** Device settings supplied by the desktop platform. */
  extraDeviceTabs?: ExtraSettingsTab[];
}

type SettingsEntry = ExtraSettingsTab & { wide?: boolean };

export function SettingsPage({ extraDeviceTabs = [] }: SettingsPageProps = {}) {
  const { t } = useT("settings");
  const workspaceName =
    useCurrentWorkspace()?.name ?? t(($) => $.page.workspace_fallback);
  const navigation = useNavigation();
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  const entry = (
    value: string,
    label: string,
    icon: ExtraSettingsTab["icon"],
    content: React.ReactNode,
    wide = false,
  ): SettingsEntry => ({ value, label, icon, content, wide });
  const groups = [
    {
      key: "personal",
      label: t(($) => $.page.groups.personal),
      scope: t(($) => $.page.groups.personal),
      entries: [
        entry(
          "profile",
          t(($) => $.page.tabs.profile),
          User,
          <AccountTab />,
        ),
        entry(
          "preferences",
          t(($) => $.page.tabs.preferences),
          SlidersHorizontal,
          <PreferencesTab />,
        ),
        entry(
          "notifications",
          t(($) => $.page.tabs.notifications),
          Bell,
          <NotificationsTab />,
        ),
        entry(
          "shortcuts",
          t(($) => $.page.tabs.shortcuts),
          Keyboard,
          <KeyboardShortcutsTab />,
        ),
        entry(
          "tokens",
          t(($) => $.page.tabs.tokens),
          Key,
          <TokensTab />,
        ),
      ],
    },
    {
      key: "workspace",
      label: t(($) => $.page.groups.workspace),
      scope: workspaceName,
      entries: [
        entry(
          "workspace",
          t(($) => $.page.tabs.general),
          Settings,
          <WorkspaceTab />,
        ),
        entry(
          "members",
          t(($) => $.page.tabs.members),
          Users,
          <MembersTab />,
        ),
        ...(billingEnabled
          ? [
              entry(
                "billing",
                t(($) => $.page.tabs.billing),
                CreditCard,
                <BillingTab />,
              ),
            ]
          : []),
      ],
    },
    {
      key: "issues",
      label: t(($) => $.page.groups.issues),
      scope: workspaceName,
      entries: [
        entry(
          "issue-statuses",
          t(($) => $.page.tabs.issue_statuses),
          CircleDot,
          <IssueStatusesTab />,
          true,
        ),
        entry(
          "labels",
          t(($) => $.page.tabs.labels),
          Tags,
          <LabelsTab />,
          true,
        ),
        entry(
          "properties",
          t(($) => $.page.tabs.properties),
          SlidersHorizontal,
          <PropertiesTab />,
          true,
        ),
        entry(
          "quick-actions",
          t(($) => $.page.tabs.quick_actions),
          Zap,
          <QuickActionsTab />,
          true,
        ),
      ],
    },
    {
      key: "connections",
      label: t(($) => $.page.groups.connections),
      scope: workspaceName,
      entries: [
        entry(
          "repositories",
          t(($) => $.page.tabs.repositories),
          FolderGit2,
          <RepositoriesTab />,
        ),
        entry(
          "integrations",
          t(($) => $.page.tabs.integrations),
          Plug,
          <IntegrationsTab />,
        ),
        entry(
          "mcp",
          t(($) => $.page.tabs.mcp),
          Server,
          <McpTab />,
        ),
        ...(pluginsEnabled
          ? [
              entry(
                "plugins",
                t(($) => $.page.tabs.plugins),
                Blocks,
                <PluginsTab />,
              ),
            ]
          : []),
      ],
    },
    ...(extraDeviceTabs.length
      ? [
          {
            key: "device",
            label: t(($) => $.page.groups.device),
            scope: t(($) => $.page.device_scope),
            entries: extraDeviceTabs as SettingsEntry[],
          },
        ]
      : []),
  ];
  const location = resolveSettingsLocation(navigation.searchParams);
  const candidate =
    location.tab === "billing" && !billingEnabled ? "workspace" : location.tab;
  const active =
    groups
      .flatMap((group) => group.entries)
      .find((item) => item.value === candidate) ?? groups[0]!.entries[0]!;
  const activeGroup = groups.find((group) => group.entries.includes(active))!;
  const href = (value: string) =>
    settingsHref(navigation.pathname, navigation.searchParams, value);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background md:flex-row">
      <aside className="shrink-0 border-b border-surface-border md:flex md:w-60 md:flex-col md:border-b-0 md:border-r">
        <div className="flex h-16 shrink-0 items-center gap-1 px-4 md:px-5">
          <CollapsedNavTrigger />
          <h1 className="text-title font-semibold tracking-tight">
            {t(($) => $.page.title)}
          </h1>
        </div>
        <div className="px-4 pb-4 md:hidden">
          <label className="sr-only" htmlFor="settings-navigation">
            {t(($) => $.page.navigate)}
          </label>
          <select
            id="settings-navigation"
            className="h-10 w-full rounded-lg border border-input bg-background px-3 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring"
            value={active.value}
            onChange={(event) => navigation.push(href(event.target.value))}
          >
            {groups.map((group) => (
              <optgroup key={group.key} label={group.label}>
                {group.entries.map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        </div>
        <nav
          aria-label={t(($) => $.page.title)}
          className="hidden min-h-0 space-y-5 overflow-y-auto px-3 pb-6 md:block"
        >
          {groups.map((group) => (
            <section
              key={group.key}
              aria-labelledby={`settings-group-${group.key}`}
            >
              <h2
                id={`settings-group-${group.key}`}
                className="mb-1.5 px-3 text-caption font-medium text-muted-foreground"
              >
                {group.label}
              </h2>
              <ul className="space-y-0.5">
                {group.entries.map((item) => (
                  <li key={item.value}>
                    <AppLink
                      href={href(item.value)}
                      aria-current={
                        active.value === item.value ? "page" : undefined
                      }
                      className={cn(
                        "flex min-h-9 items-center gap-2.5 rounded-lg px-3 py-2 text-body transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                        active.value === item.value
                          ? "bg-surface-selected font-medium text-surface-selected-foreground hover:bg-surface-selected"
                          : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
                      )}
                    >
                      <item.icon
                        className="size-4 shrink-0"
                        aria-hidden="true"
                      />
                      <span className="min-w-0 truncate">{item.label}</span>
                    </AppLink>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </nav>
      </aside>
      <div
        key={`${active.value}:${location.integration ?? ""}`}
        className="min-w-0 flex-1 overflow-y-auto overscroll-contain"
      >
        <div
          className={cn(
            "mx-auto w-full px-4 py-6 sm:px-6 md:px-10 md:py-8",
            active.wide ? "max-w-5xl" : "max-w-4xl",
          )}
        >
          <div className="mb-3 flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
            <span className="truncate">{activeGroup.scope}</span>
            {activeGroup.scope !== activeGroup.label && (
              <>
                <ChevronRight aria-hidden="true" className="size-3 shrink-0" />
                <span className="shrink-0">{activeGroup.label}</span>
              </>
            )}
          </div>
          {active.content}
        </div>
      </div>
    </div>
  );
}
