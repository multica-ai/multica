"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { SlidersHorizontal } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { BUILTIN_VIEW_KEYS, builtinViewItemId } from "@multica/core/issue-views/sidebar-views";
import { shownSidebarStatuses } from "@multica/core/issues/sidebar-list";
import { useSidebarProjectPrefs, useUpdateSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
  type SidebarSectionLimit,
  type SidebarIssueAssignee,
  type SidebarIssueSettings,
  type SidebarIssueSort,
  type SidebarProjectOrdering,
  type SidebarProjectSettings,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import { PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import type { NotificationGroupKey, NotificationPreferences, ProjectStatus } from "@multica/core/types";
import { DisplayChip, SortDirectionButton } from "../../issues/components/display-parts";
import { StatusIcon } from "../../issues/components/status-icon";
import { sortDirectionLabelKey } from "../../issues/utils/sort-direction";
import { useProjectStatusLabels } from "../../projects/components/labels";
import type { IssueViewActions } from "../../issue-views/use-view-actions";
import { useBuiltinViewLabel } from "../../issue-views/builtin-views";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { TREE_ROW_BUTTON_CLASS } from "./sidebar-tree";

/** A section's settings button shows while the pointer or focus is in the section. */
export const SECTION_SETTINGS_REVEAL_CLASS =
  "opacity-0 group-hover/section:opacity-100 group-focus-within/section:opacity-100 [@media(pointer:coarse)]:opacity-100";

const ISSUE_SORTS: {
  value: SidebarIssueSort;
  label: "sort_updated" | "sort_created" | "sort_priority" | "sort_status" | "sort_title" | "sort_due_date";
}[] = [
  { value: "updated_at", label: "sort_updated" },
  { value: "created_at", label: "sort_created" },
  { value: "priority", label: "sort_priority" },
  { value: "status", label: "sort_status" },
  { value: "title", label: "sort_title" },
  { value: "due_date", label: "sort_due_date" },
];
const PROJECT_ORDERINGS: SidebarProjectOrdering[] = ["manual", "name", "updated", "created"];
const SECTION_LIMITS: SidebarSectionLimit[] = [5, 10, 20];
const LIMIT_OPTIONS = SECTION_LIMITS.map((limit) => ({ value: String(limit), label: String(limit) }));
/** The notification kinds a member can turn off, as in Settings → Notifications. */
const INBOX_NOTIFICATION_GROUPS = [
  "assignments",
  "status_changes",
  "comments",
  "mentions",
  "reactions",
  "updates",
  "new_issues",
  "agent_activity",
] as const satisfies readonly NotificationGroupKey[];

const sameSettings = <T,>(a: T, b: T) => JSON.stringify(a) === JSON.stringify(b);

function Caption({ children }: { children: React.ReactNode }) {
  return <span className="text-caption font-medium text-muted-foreground">{children}</span>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Caption>{label}</Caption>
      {children}
    </div>
  );
}

function SwitchField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-3">
      <Caption>{label}</Caption>
      <Switch size="sm" checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

function SortSelect<T extends string>({
  label,
  value,
  options,
  onChange,
  className,
}: {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
  className?: string;
}) {
  return (
    <Select
      items={options}
      value={value}
      onValueChange={(next) => {
        if (next) onChange(next as T);
      }}
    >
      <SelectTrigger size="sm" className={className} aria-label={label}>
        <SelectValue>
          <span className="truncate">{options.find((o) => o.value === value)?.label}</span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent align="end" className="w-auto min-w-(--anchor-width)">
        <SelectGroup>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function ProjectsGroup({
  settings,
  update,
}: {
  settings: SidebarProjectSettings;
  update: (patch: Partial<SidebarProjectSettings>) => void;
}) {
  const { t } = useT("layout");
  const statusLabels = useProjectStatusLabels();
  const toggleStatus = (status: ProjectStatus, on: boolean) =>
    update({ statuses: PROJECT_STATUS_ORDER.filter((s) => (s === status ? on : settings.statuses.includes(s))) });

  return (
    <section className="space-y-3">
      <h3 className="text-label font-medium text-foreground">{t(($) => $.nav.projects)}</h3>
      <Field label={t(($) => $.panels.work.projects_ordering)}>
        <SortSelect
          label={t(($) => $.panels.work.projects_ordering)}
          value={settings.ordering}
          options={PROJECT_ORDERINGS.map((value) => ({
            value,
            label: {
              manual: t(($) => $.panels.work.projects_order_manual),
              name: t(($) => $.panels.work.projects_order_name),
              updated: t(($) => $.panels.work.projects_order_updated),
              created: t(($) => $.panels.work.projects_order_created),
            }[value],
          }))}
          onChange={(ordering) => update({ ordering })}
          className="w-36"
        />
      </Field>
      <div>
        <Caption>{t(($) => $.panels.work.projects_show)}</Caption>
        <div className="mt-2 flex flex-wrap gap-1">
          {PROJECT_STATUS_ORDER.map((status) => (
            <DisplayChip
              key={status}
              pressed={settings.statuses.includes(status)}
              onPressedChange={(on) => toggleStatus(status, on)}
            >
              <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", PROJECT_STATUS_CONFIG[status].dotColor)} />
              <span className="truncate">{statusLabels[status]}</span>
            </DisplayChip>
          ))}
        </div>
      </div>
      <SwitchField
        label={t(($) => $.panels.work.projects_only_mine)}
        checked={settings.onlyMine}
        onChange={(onlyMine) => update({ onlyMine })}
      />
      <SwitchField
        label={t(($) => $.panels.work.projects_show_no_project)}
        checked={settings.showNoProject}
        onChange={(showNoProject) => update({ showNoProject })}
      />
      <SwitchField
        label={t(($) => $.panels.work.projects_show_count)}
        checked={settings.showCount}
        onChange={(showCount) => update({ showCount })}
      />
      <SwitchField
        label={t(($) => $.panels.work.projects_minimal_icons)}
        checked={settings.minimalIcons}
        onChange={(minimalIcons) => update({ minimalIcons })}
      />
    </section>
  );
}

function ProjectIssuesGroup({
  wsId,
  settings,
  update,
}: {
  wsId: string;
  settings: SidebarIssueSettings;
  update: (patch: Partial<SidebarIssueSettings>) => void;
}) {
  const { t } = useT("layout");
  const { t: ti } = useT("issues");
  const statusCatalog = useIssueStatuses(wsId);
  const shown = new Set(shownSidebarStatuses(settings, statusCatalog.statuses));
  const toggleStatus = (key: string, on: boolean) =>
    update({
      hiddenStatuses: statusCatalog.statuses.map((s) => s.key).filter((k) => (k === key ? !on : !shown.has(k))),
    });
  const directionLabel = ti(($) => $.display[sortDirectionLabelKey(settings.sortBy, settings.sortDirection)]);
  const assigneeOptions: { value: SidebarIssueAssignee; label: string }[] = [
    { value: "anyone", label: t(($) => $.panels.work.assignee_anyone) },
    { value: "me", label: t(($) => $.panels.work.assignee_me) },
    { value: "my_agents", label: t(($) => $.panels.work.assignee_my_agents) },
    { value: "me_and_my_agents", label: t(($) => $.panels.work.assignee_me_and_my_agents) },
  ];

  return (
    <section className="mt-5 space-y-3">
      <h3 className="text-label font-medium text-foreground">{t(($) => $.panels.work.project_issues_settings)}</h3>
      <Field label={ti(($) => $.display.ordering_section)}>
        <div className="flex min-w-0 items-center gap-1">
          <SortDirectionButton
            label={directionLabel}
            onClick={() => update({ sortDirection: settings.sortDirection === "asc" ? "desc" : "asc" })}
          />
          <SortSelect
            label={ti(($) => $.display.ordering_section)}
            value={settings.sortBy}
            options={ISSUE_SORTS.map((o) => ({ value: o.value, label: ti(($) => $.display[o.label]) }))}
            onChange={(sortBy) => update({ sortBy })}
            className="w-36"
          />
        </div>
      </Field>
      <div>
        <Caption>{t(($) => $.panels.work.settings_status)}</Caption>
        <div className="mt-2 flex flex-wrap gap-1">
          {statusCatalog.activeStatuses.map((status) => (
            <DisplayChip
              key={status.key}
              pressed={shown.has(status.key)}
              onPressedChange={(on) => toggleStatus(status.key, on)}
            >
              <StatusIcon
                status={status.key}
                category={status.category}
                color={statusCatalog.colorOf(status.key)}
                icon={statusCatalog.iconOf(status.key)}
                className="size-3 shrink-0"
              />
              <span className="truncate">{statusCatalog.labelOf(status.key)}</span>
            </DisplayChip>
          ))}
        </div>
      </div>
      <div className="space-y-1">
        <Field label={t(($) => $.panels.work.settings_assignee)}>
          <SortSelect
            label={t(($) => $.panels.work.settings_assignee)}
            value={settings.assignee}
            options={assigneeOptions}
            onChange={(assignee) => update({ assignee })}
            className="w-44"
          />
        </Field>
        <p className="text-caption text-muted-foreground">{t(($) => $.panels.work.assignee_projects_only)}</p>
      </div>
      <SwitchField
        label={ti(($) => $.display.show_sub_issues)}
        checked={settings.showSubIssues}
        onChange={(showSubIssues) => update({ showSubIssues })}
      />
    </section>
  );
}

/**
 * Display settings for the whole Projects section: which projects it lists,
 * and how every expanded project lists its issues.
 */
export function ProjectsSectionSettings({ wsId, className }: { wsId: string; className?: string }) {
  const { t } = useT("layout");
  const { projectSettings, issueSettings } = useSidebarProjectPrefs(wsId);
  const update = useUpdateSidebarProjectPrefs(wsId);
  const changed =
    !sameSettings(projectSettings, DEFAULT_SIDEBAR_PROJECT_SETTINGS) ||
    !sameSettings(issueSettings, DEFAULT_SIDEBAR_ISSUE_SETTINGS);
  const reset = () =>
    update({ projectSettings: DEFAULT_SIDEBAR_PROJECT_SETTINGS, issueSettings: DEFAULT_SIDEBAR_ISSUE_SETTINGS });

  return (
    <SectionSettingsPopover
      label={t(($) => $.panels.work.projects_settings)}
      className={className}
      onReset={changed ? reset : undefined}
    >
      <ProjectsGroup settings={projectSettings} update={(patch) => update({ projectSettings: patch })} />
      <ProjectIssuesGroup
        wsId={wsId}
        settings={issueSettings}
        update={(patch) => update({ issueSettings: patch })}
      />
    </SectionSettingsPopover>
  );
}

/** The sliders button at the end of a section's label row, and its popover. */
function SectionSettingsPopover({
  label,
  className,
  onReset,
  children,
}: {
  label: string;
  className?: string;
  /** Offered while the section's settings differ from the defaults. */
  onReset?: () => void;
  children: React.ReactNode;
}) {
  const { t } = useT("layout");
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label={label}
            className={cn(
              TREE_ROW_BUTTON_CLASS,
              "text-muted-foreground data-popup-open:bg-surface-selected data-popup-open:text-foreground data-popup-open:opacity-100",
              className,
            )}
          />
        }
      >
        <SlidersHorizontal className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent
        side="right"
        align="start"
        sideOffset={8}
        className="max-h-(--available-height) w-80 gap-0 overflow-y-auto p-3"
      >
        {/* Spacing separates sections — no dividers, as in the Display panel. */}
        {children}
        {onReset && (
          <div className="-mr-1.5 mt-4 flex justify-end">
            <Button variant="ghost" size="sm" className="text-muted-foreground" onClick={onReset}>
              {t(($) => $.panels.work.reset)}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function LimitField({ value, onChange }: { value: SidebarSectionLimit; onChange: (limit: SidebarSectionLimit) => void }) {
  const { t } = useT("layout");
  return (
    <Field label={t(($) => $.panels.work.section_limit)}>
      <SortSelect
        label={t(($) => $.panels.work.section_limit)}
        value={String(value)}
        options={LIMIT_OPTIONS}
        onChange={(next) => onChange(Number(next) as SidebarSectionLimit)}
        className="w-20"
      />
    </Field>
  );
}

/**
 * Which notifications reach the inbox at all: the account's notification
 * preferences for this workspace. A kind turned off is not delivered while it
 * is off; turning it back on does not bring those back. Mounted only while
 * the popover is open.
 */
function InboxNotificationTypes() {
  const { t } = useT("layout");
  const { t: ts } = useT("settings");
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const updatePreferences = useUpdateNotificationPreferences();
  const preferences = data?.preferences ?? {};
  const toggleGroup = (key: NotificationGroupKey, enabled: boolean) => {
    const next: NotificationPreferences = { ...preferences, [key]: enabled ? "all" : "muted" };
    // "all" is the default; the stored object keeps only muted kinds.
    if (enabled) delete next[key];
    updatePreferences.mutate(next, {
      onError: (err) =>
        toast.error(err instanceof Error && err.message ? err.message : ts(($) => $.notifications.toast_failed)),
    });
  };

  return (
    <section className="mt-5 space-y-3">
      <div className="space-y-1">
        <h3 className="text-label font-medium text-foreground">{t(($) => $.panels.work.inbox_types)}</h3>
        <p className="text-caption text-muted-foreground">{t(($) => $.panels.work.inbox_types_hint)}</p>
      </div>
      {INBOX_NOTIFICATION_GROUPS.map((key) => (
        <SwitchField
          key={key}
          label={ts(($) => $.notifications.groups[key].label)}
          checked={preferences[key] !== "muted"}
          onChange={(enabled) => toggleGroup(key, enabled)}
        />
      ))}
      <AppLink
        href={`${p.settings()}?tab=notifications`}
        className="inline-block text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        {t(($) => $.panels.work.inbox_more_settings)}
      </AppLink>
    </section>
  );
}

/** The Inbox section's display, and which notifications reach the inbox. */
export function InboxSectionSettings({ className }: { className?: string }) {
  const { t } = useT("layout");
  const settings = useSidebarTreeStore((s) => s.inboxSettings);
  const update = useSidebarTreeStore((s) => s.setInboxSettings);

  return (
    <SectionSettingsPopover
      label={t(($) => $.panels.work.inbox_settings)}
      className={className}
      onReset={sameSettings(settings, DEFAULT_SIDEBAR_INBOX_SETTINGS) ? undefined : () => update(DEFAULT_SIDEBAR_INBOX_SETTINGS)}
    >
      <section className="space-y-3">
        <h3 className="text-label font-medium text-foreground">{t(($) => $.nav.inbox)}</h3>
        <LimitField value={settings.limit} onChange={(limit) => update({ limit })} />
        <SwitchField
          label={t(($) => $.panels.work.inbox_compact)}
          checked={settings.compact}
          onChange={(compact) => update({ compact })}
        />
        <SwitchField
          label={t(($) => $.panels.work.inbox_show_count)}
          checked={settings.showCount}
          onChange={(showCount) => update({ showCount })}
        />
        <SwitchField
          label={t(($) => $.panels.work.inbox_rail_badge)}
          checked={settings.railBadge}
          onChange={(railBadge) => update({ railBadge })}
        />
      </section>
      <InboxNotificationTypes />
    </SectionSettingsPopover>
  );
}

export function PinnedSectionSettings({ className }: { className?: string }) {
  const { t } = useT("layout");
  const limit = useSidebarTreeStore((s) => s.pinnedLimit);
  const setLimit = useSidebarTreeStore((s) => s.setPinnedLimit);
  return (
    <SectionSettingsPopover
      label={t(($) => $.panels.work.pinned_settings)}
      className={className}
      onReset={limit === 5 ? undefined : () => setLimit(5)}
    >
      <section className="space-y-3">
        <h3 className="text-label font-medium text-foreground">{t(($) => $.sidebar.pinned_label)}</h3>
        <LimitField value={limit} onChange={setLimit} />
      </section>
    </SectionSettingsPopover>
  );
}

/** Which built-in views the Views section lists. Mounted only while the popover is open. */
function BuiltinViewSwitches({ sidebar }: { sidebar: IssueViewActions["sidebar"] }) {
  const { t } = useT("layout");
  const builtinLabel = useBuiltinViewLabel();
  return (
    <section className="space-y-3">
      <h3 className="text-label font-medium text-foreground">{t(($) => $.panels.work.views_builtin)}</h3>
      {BUILTIN_VIEW_KEYS.map((key) => {
        const id = builtinViewItemId(key);
        return (
          <SwitchField
            key={key}
            label={builtinLabel(key)}
            checked={sidebar.isStarred(id)}
            onChange={(on) => (on ? sidebar.star(id) : sidebar.unstar(id))}
          />
        );
      })}
    </section>
  );
}

/** Which built-in views the Views section lists, and whether its rows show counts. */
export function ViewsSectionSettings({
  sidebar,
  className,
}: {
  sidebar: IssueViewActions["sidebar"];
  className?: string;
}) {
  const { t } = useT("layout");
  const showCount = useSidebarTreeStore((s) => s.viewsShowCount);
  const setShowCount = useSidebarTreeStore((s) => s.setViewsShowCount);
  return (
    <SectionSettingsPopover label={t(($) => $.panels.work.views_settings)} className={className}>
      <BuiltinViewSwitches sidebar={sidebar} />
      <section className="mt-5 space-y-3">
        <SwitchField label={t(($) => $.panels.work.views_show_count)} checked={showCount} onChange={setShowCount} />
      </section>
    </SectionSettingsPopover>
  );
}
