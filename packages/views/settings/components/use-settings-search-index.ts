"use client";

import { useMemo } from "react";
import { useT } from "../../i18n";
import type { SettingsSearchEntry } from "./settings-search";
import { CHANNEL_INTEGRATIONS } from "./settings-navigation";

const NOTIFICATION_GROUPS = [
  "assignments",
  "status_changes",
  "comments",
  "mentions",
  "updates",
  "agent_activity",
] as const;

export interface SettingsSearchPage {
  value: string;
  label: string;
}

export type SettingsSearchIndexEntry = SettingsSearchEntry;

/**
 * The searchable settings for the pages currently in the navigation. Anchors
 * here must match the `anchor` props on the corresponding `SettingsSection` /
 * `SettingsRow`; entries for hidden pages (feature flags, desktop-only) are
 * dropped so a result never lands on a page the user cannot open.
 */
export function useSettingsSearchIndex(
  pages: readonly SettingsSearchPage[],
): SettingsSearchIndexEntry[] {
  const { t } = useT("settings");
  return useMemo(() => {
    const visible = new Set(pages.map((page) => page.value));
    const rows: SettingsSearchIndexEntry[] = [
      // Personal
      { tab: "profile", anchor: "avatar", title: t(($) => $.account.avatar_label) },
      { tab: "profile", anchor: "name", title: t(($) => $.account.name_label) },
      {
        tab: "profile",
        anchor: "about",
        title: t(($) => $.account.profile_description_label),
        description: t(($) => $.account.profile_description_hint),
      },
      { tab: "preferences", anchor: "language", title: t(($) => $.preferences.language.title) },
      {
        tab: "preferences",
        anchor: "timezone",
        title: t(($) => $.preferences.timezone.title),
        description: t(($) => $.preferences.timezone.hint),
      },
      { tab: "preferences", anchor: "theme", title: t(($) => $.preferences.theme.title) },
      {
        tab: "preferences",
        anchor: "sticky-comment-bar",
        title: t(($) => $.preferences.sticky_comment_bar.title),
      },
      {
        tab: "preferences",
        anchor: "running-agent-reply",
        title: t(($) => $.preferences.running_agent_reply.title),
        description: t(($) => $.preferences.running_agent_reply.hint),
      },
      {
        tab: "preferences",
        anchor: "chat",
        title: t(($) => $.chat.floating_label),
        description: t(($) => $.chat.floating_hint),
      },
      {
        tab: "preferences",
        anchor: "issue",
        title: t(($) => $.preferences.issue_fields_title),
        description: t(($) => $.issue.description),
      },
      ...NOTIFICATION_GROUPS.map((group) => ({
        tab: "notifications",
        anchor: group,
        title: t(($) => $.notifications.groups[group].label),
        description: t(($) => $.notifications.groups[group].description),
      })),
      {
        tab: "notifications",
        anchor: "system",
        title: t(($) => $.notifications.system.label),
        description: t(($) => $.notifications.system.description),
      },
      {
        tab: "notifications",
        anchor: "browser",
        title: t(($) => $.notifications.browser.label),
      },
      {
        tab: "tokens",
        title: t(($) => $.page.tabs.tokens),
        description: t(($) => $.tokens.purpose),
      },
      {
        tab: "shortcuts",
        title: t(($) => $.page.tabs.shortcuts),
        description: t(($) => $.shortcuts.description),
      },
      {
        tab: "apps",
        title: t(($) => $.page.tabs.apps),
        description: t(($) => $.composio.page_description),
      },
      // Workspace
      { tab: "workspace", anchor: "name", title: t(($) => $.workspace.name_label) },
      { tab: "workspace", anchor: "description", title: t(($) => $.workspace.description_label) },
      {
        tab: "workspace",
        anchor: "context",
        title: t(($) => $.workspace.context_label),
        description: t(($) => $.workspace.context_hint),
      },
      { tab: "workspace", anchor: "slug", title: t(($) => $.workspace.slug_label) },
      {
        tab: "workspace",
        anchor: "issue-prefix",
        title: t(($) => $.workspace.issue_prefix_label),
      },
      { tab: "workspace", anchor: "leave", title: t(($) => $.workspace.leave_title) },
      { tab: "workspace", anchor: "delete", title: t(($) => $.workspace.delete_title) },
      { tab: "members", anchor: "invitations", title: t(($) => $.members.pending_label) },
      { tab: "members", anchor: "links", title: t(($) => $.members.share_links_label) },
      {
        tab: "issue-statuses",
        anchor: "auto-transitions",
        title: t(($) => $.issue_statuses.auto_transitions.title),
      },
      {
        tab: "issue-statuses",
        anchor: "pr-merge-status",
        title: t(($) => $.pr_merge_status.label),
        description: t(($) => $.pr_merge_status.description),
      },
      {
        tab: "properties",
        title: t(($) => $.page.tabs.properties),
        description: t(($) => $.properties.description),
      },
      {
        tab: "quick-actions",
        title: t(($) => $.page.tabs.quick_actions),
        description: t(($) => $.quick_actions.description),
      },
      { tab: "code", anchor: "code-hosting", title: t(($) => $.code.hosting_title) },
      { tab: "code", anchor: "github", title: t(($) => $.page.tabs.github) },
      { tab: "code", anchor: "vcs", title: t(($) => $.vcs.section_title) },
      {
        tab: "code",
        anchor: "repositories",
        title: t(($) => $.repositories.section_title),
        description: t(($) => $.repositories.description),
      },
      { tab: "code", anchor: "pr-linking", title: t(($) => $.code.pr_linking_title) },
      {
        tab: "code",
        anchor: "pr-sidebar",
        title: t(($) => $.github.feature_pr_sidebar_label),
        description: t(($) => $.github.feature_pr_sidebar_description),
      },
      {
        tab: "code",
        anchor: "auto-link",
        title: t(($) => $.github.feature_auto_link_label),
        description: t(($) => $.github.feature_auto_link_description, {
          example: "MUL-123",
        }),
      },
      {
        tab: "code",
        anchor: "co-author",
        title: t(($) => $.github.feature_co_author_label),
      },
      ...CHANNEL_INTEGRATIONS.map((channel) => ({
        tab: "channels",
        integration: channel,
        title: t(($) => $[channel].section_title),
        description: t(($) => $[channel].page_description),
      })),
      {
        tab: "mcp",
        title: t(($) => $.page.tabs.mcp),
        description: t(($) => $.mcp.description),
      },
    ];
    // Rows without an anchor describe a whole page: fold their copy into the
    // page entry so each page is indexed once, with its description.
    const pageDescriptions = new Map(
      rows
        .filter((row) => !row.anchor && !row.integration)
        .map((row) => [row.tab, row.description]),
    );
    const pageEntries: SettingsSearchIndexEntry[] = pages.map((page) => ({
      tab: page.value,
      title: page.label,
      description: pageDescriptions.get(page.value),
    }));
    return [
      ...pageEntries,
      ...rows.filter((row) => row.anchor || row.integration),
    ].filter((entry) => visible.has(entry.tab));
  }, [pages, t]);
}
