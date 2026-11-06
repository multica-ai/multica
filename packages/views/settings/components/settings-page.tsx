"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  AlarmClock,
  Bell,
  Blocks,
  CreditCard,
  FolderGit2,
  Keyboard,
  KeyRound,
  ListPlus,
  Lock,
  MessagesSquare,
  Plug,
  Search,
  Server,
  Settings,
  SlidersHorizontal,
  Tags,
  User,
  Users,
  X,
  Workflow,
  Zap,
} from "@multica/ui/icons";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureEnabled } from "@multica/core/config";
import { useCurrentMember } from "@multica/core/permissions";
import {
  BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
  PLUGINS_V1_FLAG,
} from "@multica/core/feature-flags";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { cn } from "@multica/ui/lib/utils";
import { resolveSettingsLocation, settingsHref } from "./settings-navigation";
import { AppLink, useNavigation } from "../../navigation";
import { AccountTab } from "./account-tab";
import { PreferencesTab } from "./preferences-tab";
import { TokensTab } from "./tokens-tab";
import { WorkspaceTab } from "./workspace-tab";
import { MembersTab } from "./members-tab";
import { CodeTab } from "./code-tab";
import { ChannelsTab } from "./channels-tab";
import { ConnectedAppsTab, useComposioAvailable } from "./connected-apps-tab";
import { NotificationsTab } from "./notifications-tab";
import { LabelsTab } from "./labels-tab";
import { WorkflowsTab } from "../../workflows/workflows-tab";
import { PropertiesTab } from "./properties-tab";
import { QuickActionsTab } from "./quick-actions-tab";
import { KeyboardShortcutsTab } from "./keyboard-shortcuts-tab";
import { PluginsTab } from "./plugins-tab";
import { McpTab } from "./mcp-tab";
import { BillingTab } from "./billing-tab";
import {
  SETTINGS_ANCHOR_ATTR,
  SettingsHeaderContext,
  type SettingsHeaderSlots,
} from "./settings-layout";
import { searchSettings } from "./settings-search";
import { HighlightText } from "../../search/highlight-text";
import { useSettingsSearchIndex } from "./use-settings-search-index";
import { WakeupsTab } from "./wakeups-tab";
import { CollapsedNavTrigger, PageHeader } from "../../layout/page-header";
import { PanelHeader } from "../../layout/panel-nav";
import { useListColumnCollapsed } from "../../layout/app-shell-context";
import { ListColumn } from "../../layout/list-column";
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

type SettingsEntry = ExtraSettingsTab & {
  wide?: boolean;
  /** Owners and admins manage it; members see a read-only page. */
  adminOnly?: boolean;
};

interface SettingsSubgroup {
  key: string;
  label?: string;
  entries: SettingsEntry[];
}

interface SettingsScopeGroup {
  key: "personal" | "workspace" | "device";
  label: string;
  /** Nav heading of the unlabeled first subgroup, when it differs from `label`. */
  navLabel?: string;
  subgroups: SettingsSubgroup[];
}

const FLASH_MS = 1600;
const ANCHOR_WAIT_MS = 1500;

export function SettingsPage({ extraDeviceTabs = [] }: SettingsPageProps = {}) {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const workspaceName = workspace?.name ?? t(($) => $.page.workspace_fallback);
  const { role } = useCurrentMember(workspace?.id ?? "");
  const isMember = role === "member";
  const navigation = useNavigation();
  const listCollapsed = useListColumnCollapsed();
  const pluginsEnabled = useFeatureEnabled(PLUGINS_V1_FLAG, false);
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  const appsAvailable = useComposioAvailable();
  const entry = (
    value: string,
    label: string,
    icon: ExtraSettingsTab["icon"],
    content: React.ReactNode,
    options: { wide?: boolean; adminOnly?: boolean } = {},
  ): SettingsEntry => ({ value, label, icon, content, ...options });

  const groups: SettingsScopeGroup[] = [
    {
      key: "personal",
      label: t(($) => $.page.groups.personal),
      subgroups: [
        {
          key: "personal",
          entries: [
            entry("profile", t(($) => $.page.tabs.profile), User, <AccountTab />),
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
            ...(appsAvailable
              ? [entry("apps", t(($) => $.page.tabs.apps), Plug, <ConnectedAppsTab />)]
              : []),
            entry("tokens", t(($) => $.page.tabs.tokens), KeyRound, <TokensTab />),
          ],
        },
      ],
    },
    {
      key: "workspace",
      label: workspaceName,
      navLabel: t(($) => $.page.groups.workspace),
      subgroups: [
        {
          key: "workspace",
          entries: [
            entry("workspace", t(($) => $.page.tabs.general), Settings, <WorkspaceTab />, {
              adminOnly: true,
            }),
            entry("members", t(($) => $.page.tabs.members), Users, <MembersTab />, {
              adminOnly: true,
            }),
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
          entries: [
            entry("workflows", t(($) => $.page.tabs.workflows), Workflow, <WorkflowsTab />, {
              wide: true,
            }),
            entry("wakeups", t(($) => $.page.tabs.wakeups), AlarmClock, <WakeupsTab />, {
              wide: true,
              adminOnly: true,
            }),
            entry("labels", t(($) => $.page.tabs.labels), Tags, <LabelsTab />, {
              wide: true,
            }),
            entry(
              "properties",
              t(($) => $.page.tabs.properties),
              ListPlus,
              <PropertiesTab />,
              { wide: true, adminOnly: true },
            ),
            entry(
              "quick-actions",
              t(($) => $.page.tabs.quick_actions),
              Zap,
              <QuickActionsTab />,
              { wide: true },
            ),
          ],
        },
        {
          key: "connections",
          label: t(($) => $.page.groups.connections),
          entries: [
            entry("code", t(($) => $.page.tabs.code), FolderGit2, <CodeTab />, {
              adminOnly: true,
            }),
            entry(
              "channels",
              t(($) => $.page.tabs.channels),
              MessagesSquare,
              <ChannelsTab />,
            ),
            entry("mcp", t(($) => $.page.tabs.mcp), Server, <McpTab />, {
              adminOnly: true,
            }),
            ...(pluginsEnabled
              ? [
                  entry("plugins", t(($) => $.page.tabs.plugins), Blocks, <PluginsTab />, {
                    adminOnly: true,
                  }),
                ]
              : []),
          ],
        },
      ],
    },
    ...(extraDeviceTabs.length
      ? [
          {
            key: "device" as const,
            label: t(($) => $.page.groups.device),
            subgroups: [{ key: "device", entries: extraDeviceTabs as SettingsEntry[] }],
          },
        ]
      : []),
  ];
  const allEntries = groups.flatMap((group) =>
    group.subgroups.flatMap((subgroup) => subgroup.entries),
  );
  const location = resolveSettingsLocation(navigation.searchParams);
  const candidate =
    location.tab === "billing" && !billingEnabled ? "workspace" : location.tab;
  const active =
    allEntries.find((item) => item.value === candidate) ?? allEntries[0]!;
  const href = (value: string) =>
    settingsHref(navigation.pathname, navigation.searchParams, value);

  const scopeOf = (value: string) =>
    groups.find((group) =>
      group.subgroups.some((subgroup) =>
        subgroup.entries.some((item) => item.value === value),
      ),
    );

  // --- header ---------------------------------------------------------------
  const [headerTrail, setHeaderTrail] = useState<HTMLElement | null>(null);
  const [headerMeta, setHeaderMeta] = useState<HTMLElement | null>(null);
  const [headerActions, setHeaderActions] = useState<HTMLElement | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const headerSlots = useMemo<SettingsHeaderSlots>(
    () => ({
      trail: headerTrail,
      meta: headerMeta,
      actions: headerActions,
      onDetailChange: setDetailOpen,
    }),
    [headerTrail, headerMeta, headerActions],
  );
  const ActiveIcon = active.icon;

  // --- search ---------------------------------------------------------------
  const [query, setQuery] = useState("");
  const [activeResult, setActiveResult] = useState(0);
  const searchPages = useMemo(
    () => allEntries.map((item) => ({ value: item.value, label: item.label })),
    // Labels are derived from i18n and flags; the value list is the identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [allEntries.map((item) => `${item.value}:${item.label}`).join("|")],
  );
  const index = useSettingsSearchIndex(searchPages);
  const results = useMemo(() => searchSettings(index, query), [index, query]);
  const openResult = (position: number) => {
    const result = results[position];
    if (!result) return;
    setActiveResult(position);
    navigation.push(
      settingsHref(navigation.pathname, navigation.searchParams, result.tab, {
        section: result.anchor,
        integration: result.integration,
      }),
    );
  };

  // --- anchors --------------------------------------------------------------
  // Search results and legacy `?section=` links name an anchor inside the
  // page. Tab content can still be loading on the first frame, so wait briefly
  // for the anchor before scrolling it into view and flashing it.
  const contentRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const anchor = location.section;
    if (!anchor) return;
    let cancelled = false;
    let flashTimer: ReturnType<typeof setTimeout> | undefined;
    const startedAt = Date.now();
    const find = () => {
      if (cancelled) return;
      const target = contentRef.current?.querySelector<HTMLElement>(
        `[${SETTINGS_ANCHOR_ATTR}="${CSS.escape(anchor)}"]`,
      );
      if (!target) {
        if (Date.now() - startedAt < ANCHOR_WAIT_MS) {
          requestAnimationFrame(find);
        }
        return;
      }
      const reduceMotion =
        typeof window.matchMedia === "function" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      target.scrollIntoView?.({
        block: "center",
        behavior: reduceMotion ? "auto" : "smooth",
      });
      target.setAttribute("data-flash", "true");
      flashTimer = setTimeout(() => target.removeAttribute("data-flash"), FLASH_MS);
    };
    requestAnimationFrame(find);
    return () => {
      cancelled = true;
      if (flashTimer) clearTimeout(flashTimer);
    };
  }, [active.value, location.section]);

  const navItem = (item: SettingsEntry) => (
    <li key={item.value}>
      <AppLink
        href={href(item.value)}
        aria-current={active.value === item.value ? "page" : undefined}
        className={cn(
          "flex min-h-8 items-center gap-2.5 rounded-lg px-3 py-1.5 text-body transition-colors focus-visible:outline-2 focus-visible:outline-ring",
          active.value === item.value
            ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
            : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
        )}
      >
        <item.icon className="size-4 shrink-0" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate">{item.label}</span>
        {isMember && item.adminOnly ? (
          <Lock
            className="size-3 shrink-0 text-faint-foreground"
            aria-label={t(($) => $.page.admin_only)}
          />
        ) : null}
      </AppLink>
    </li>
  );

  const searchResults = (
    <div className="space-y-1">
      <p className="px-3 pb-1 text-caption text-muted-foreground" role="status">
        {results.length > 0
          ? t(($) => $.page.search_results, { count: results.length })
          : t(($) => $.page.search_empty)}
      </p>
      <ul id="settings-search-results" role="listbox" aria-label={t(($) => $.page.search)}>
        {results.map((result, position) => {
          const page = allEntries.find((item) => item.value === result.tab);
          const scope = scopeOf(result.tab);
          const path = [scope?.label, result.anchor || result.integration ? page?.label : null]
            .filter(Boolean)
            .join(" › ");
          return (
            <li key={`${result.tab}#${result.anchor ?? ""}#${result.integration ?? ""}`}>
              <button
                type="button"
                role="option"
                aria-selected={position === activeResult}
                onClick={() => openResult(position)}
                onMouseMove={() => setActiveResult(position)}
                className={cn(
                  "w-full rounded-lg px-3 py-2 text-left transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                  position === activeResult
                    ? "bg-surface-selected text-surface-selected-foreground"
                    : "hover:bg-surface-hover",
                )}
              >
                <span className="block truncate text-body">
                  <HighlightText text={result.title} query={query} />
                </span>
                {result.snippet ? (
                  <span className="mt-0.5 block truncate text-caption text-muted-foreground">
                    <HighlightText text={result.snippet} query={query} />
                  </span>
                ) : null}
                <span className="mt-0.5 block truncate text-caption text-muted-foreground">
                  {path}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background md:flex-row">
      <ListColumn
        collapsed={listCollapsed}
        // Below md the navigation stacks above the content as a select.
        className="max-md:h-auto max-md:w-full"
        innerClassName="border-surface-border max-md:h-auto max-md:w-full max-md:border-r-0 max-md:border-b"
      >
        <PanelHeader as="h1" title={t(($) => $.page.title)} leading={<CollapsedNavTrigger />} />
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
            {groups.flatMap((group) =>
              group.subgroups.map((subgroup) => (
                <optgroup
                  key={`${group.key}:${subgroup.key}`}
                  label={subgroup.label ? `${group.label} · ${subgroup.label}` : group.label}
                >
                  {subgroup.entries.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </optgroup>
              )),
            )}
          </select>
        </div>
        <div className="relative mx-3 mb-3 hidden shrink-0 md:block">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <input
            type="search"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActiveResult(0);
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return;
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setActiveResult((current) =>
                  Math.min(current + 1, Math.max(results.length - 1, 0)),
                );
              } else if (event.key === "ArrowUp") {
                event.preventDefault();
                setActiveResult((current) => Math.max(current - 1, 0));
              } else if (event.key === "Enter") {
                event.preventDefault();
                openResult(activeResult);
              } else if (event.key === "Escape" && query) {
                event.preventDefault();
                setQuery("");
              }
            }}
            placeholder={t(($) => $.page.search)}
            aria-label={t(($) => $.page.search)}
            aria-controls={query ? "settings-search-results" : undefined}
            className="h-8 w-full rounded-lg border border-input bg-transparent pl-8 pr-8 text-body outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 [&::-webkit-search-cancel-button]:hidden"
          />
          {query ? (
            <TooltipHint label={t(($) => $.page.search_clear)}>
              <button
                type="button"
                onClick={() => setQuery("")}
                aria-label={t(($) => $.page.search_clear)}
                className="absolute right-1.5 top-1/2 flex size-6 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-hover hover:text-foreground"
              >
                <X className="size-3.5" aria-hidden="true" />
              </button>
            </TooltipHint>
          ) : null}
        </div>
        <nav
          aria-label={t(($) => $.page.title)}
          className="hidden min-h-0 overflow-y-auto px-3 pb-6 md:block"
        >
          {query ? (
            searchResults
          ) : (
            // One level of headings: each page names its own scope in a badge,
            // so the nav only groups. Same section rhythm as the space panels.
            groups.flatMap((group) =>
              group.subgroups.map((subgroup) => {
                const id = `settings-group-${group.key}-${subgroup.key}`;
                return (
                  <section key={id} aria-labelledby={id} className="mt-5 first:mt-0">
                    <h2
                      id={id}
                      className="flex h-7 min-w-0 items-center px-3 text-caption font-medium text-muted-foreground"
                    >
                      <span className="truncate">
                        {subgroup.label ?? group.navLabel ?? group.label}
                      </span>
                    </h2>
                    <ul className="space-y-px">{subgroup.entries.map(navItem)}</ul>
                  </section>
                );
              }),
            )
          )}
        </nav>
      </ListColumn>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PageHeader leading={listCollapsed ? <CollapsedNavTrigger /> : false}>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <h2 className="flex min-w-0 items-center gap-2 px-2 text-body font-medium">
              <ActiveIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              {detailOpen ? (
                <AppLink
                  href={href(active.value)}
                  className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
                >
                  {active.label}
                </AppLink>
              ) : (
                <span className="truncate">{active.label}</span>
              )}
              <span ref={setHeaderTrail} className="contents" />
            </h2>
            <span ref={setHeaderMeta} className="contents" />
          </div>
          <div ref={setHeaderActions} className="flex shrink-0 items-center gap-2 empty:hidden" />
        </PageHeader>
        <SettingsHeaderContext.Provider value={headerSlots}>
          <div
            ref={contentRef}
            key={`${active.value}:${location.integration ?? ""}`}
            className="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain"
          >
            <div
              className={cn(
                "mx-auto w-full px-4 py-6 sm:px-6 md:px-10 md:py-8",
                active.wide ? "max-w-5xl" : "max-w-4xl",
              )}
            >
              {active.content}
            </div>
          </div>
        </SettingsHeaderContext.Provider>
      </div>
    </div>
  );
}
