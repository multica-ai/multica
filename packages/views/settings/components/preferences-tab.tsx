"use client";

import { useMemo, useState } from "react";
import { ChevronsUpDown } from "lucide-react";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import {
  DEFAULT_LOCALE,
  SUPPORTED_LOCALES,
  type SupportedLocale,
} from "@multica/core/i18n";
import { useLocaleAdapter } from "@multica/core/i18n/react";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import {
  useCommentComposerStore,
  type RunningAgentReply,
} from "@multica/core/issues/stores";
import {
  MANUAL_CREATE_FIELDS,
  QUICK_CREATE_FIELDS,
  useIssueCreateSettingsStore,
  type ManualCreateField,
  type QuickCreateField,
} from "@multica/core/issues/stores/issue-create-settings-store";
import { api } from "@multica/core/api";
import { browserTimezone, timezoneOptions } from "../../common/timezone-select";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Preferences on one page. The settings here are stored in three different
 * places — the account (language, timezone), this device (theme, composer,
 * chat) and this device for the current workspace (create-dialog fields) —
 * so each section carries a scope badge instead of splitting them into tabs.
 * Changes apply immediately; only failures are announced.
 */
export function PreferencesTab() {
  const { t } = useT("settings");
  return (
    <SettingsTab title={t(($) => $.page.tabs.preferences)}>
      <SettingsSection
        title={t(($) => $.preferences.region_title)}
        scope="account"
        anchor="region"
      >
        <SettingsCard>
          <LanguageRow />
          <TimezoneRow />
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.preferences.appearance_title)}
        scope="device"
        anchor="appearance"
      >
        <SettingsCard>
          <ThemeRow />
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.preferences.comments_title)}
        scope="device"
        anchor="comments"
      >
        <SettingsCard>
          <StickyCommentBarRow />
          <RunningAgentReplyRow />
          <FloatingChatRow />
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.preferences.issue_fields_title)}
        description={t(($) => $.issue.description)}
        scope="device-workspace"
        anchor="issue"
      >
        <IssueFieldsMatrix />
      </SettingsSection>
    </SettingsTab>
  );
}

function ThemeRow() {
  const { t } = useT("settings");
  const { theme, setTheme } = useTheme();
  const value = theme === "light" || theme === "dark" ? theme : "system";
  return (
    <SettingsRow anchor="theme" label={t(($) => $.preferences.theme.title)}>
      <div role="group" aria-label={t(($) => $.preferences.theme.title)}>
        <SegmentedToggle
          value={value}
          onChange={setTheme}
          buttonClassName="px-3 py-1 text-label"
          options={[
            ["light", t(($) => $.preferences.theme.light)],
            ["dark", t(($) => $.preferences.theme.dark)],
            ["system", t(($) => $.preferences.theme.system)],
          ]}
        />
      </div>
    </SettingsRow>
  );
}

function LanguageRow() {
  const { t, i18n } = useT("settings");
  const localeAdapter = useLocaleAdapter();
  const user = useAuthStore((s) => s.user);

  // i18next.language can be a region-tagged BCP-47 string (e.g. "en-US",
  // "zh-Hans-CN") returned by intl-localematcher. Normalize to a supported
  // locale before comparing — otherwise the select shows neither option.
  const currentLocale: SupportedLocale = SUPPORTED_LOCALES.includes(
    i18n.language as SupportedLocale,
  )
    ? (i18n.language as SupportedLocale)
    : DEFAULT_LOCALE;

  const languageOptions: { value: SupportedLocale; label: string }[] = [
    { value: "en", label: t(($) => $.preferences.language.english) },
    { value: "zh-Hans", label: t(($) => $.preferences.language.chinese) },
    { value: "ko", label: t(($) => $.preferences.language.korean) },
    { value: "ja", label: t(($) => $.preferences.language.japanese) },
    { value: "fr", label: t(($) => $.preferences.language.french) },
  ];

  // Persist locally → sync to user.language → reload. Reload (vs in-place
  // changeLanguage) avoids hydration mismatch and is the i18next-recommended
  // pattern for App Router. The reload itself is the confirmation.
  //
  // If the cross-device sync (PATCH /api/me) fails, the local cookie is
  // already written so the new locale will take effect after reload — but
  // the user's other devices won't see the change. Surface that explicitly
  // via a toast and delay the reload long enough for the toast to be read,
  // otherwise the failure would be invisible.
  const handleLanguageChange = async (next: SupportedLocale) => {
    if (next === currentLocale) return;
    localeAdapter.persist(next);

    let syncFailed = false;
    if (user) {
      try {
        await api.updateMe({ language: next });
      } catch {
        syncFailed = true;
      }
    }

    if (syncFailed) {
      toast.warning(t(($) => $.preferences.language.sync_failed));
      // Give the toast 2.5s of visible time before navigating away.
      setTimeout(() => window.location.reload(), 2500);
      return;
    }
    window.location.reload();
  };

  return (
    <SettingsRow
      anchor="language"
      label={t(($) => $.preferences.language.title)}
      size="select"
    >
      <Select
        items={languageOptions}
        value={currentLocale}
        onValueChange={(next) => {
          if (next) void handleLanguageChange(next as SupportedLocale);
        }}
      >
        <SelectTrigger
          size="sm"
          className="w-full"
          aria-label={t(($) => $.preferences.language.title)}
        >
          <SelectValue>
            {languageOptions.find((option) => option.value === currentLocale)?.label}
          </SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {languageOptions.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

function StickyCommentBarRow() {
  const { t } = useT("settings");
  const sticky = useCommentComposerStore((s) => s.sticky);
  const toggleSticky = useCommentComposerStore((s) => s.toggleSticky);

  return (
    <SettingsRow
      anchor="sticky-comment-bar"
      label={t(($) => $.preferences.sticky_comment_bar.title)}
    >
      <Switch
        checked={sticky}
        onCheckedChange={() => toggleSticky()}
        aria-label={t(($) => $.preferences.sticky_comment_bar.title)}
      />
    </SettingsRow>
  );
}

function RunningAgentReplyRow() {
  const { t } = useT("settings");
  const value = useCommentComposerStore((s) => s.runningAgentReply);
  const setValue = useCommentComposerStore((s) => s.setRunningAgentReply);
  const options: { value: RunningAgentReply; label: string }[] = [
    { value: "steer", label: t(($) => $.preferences.running_agent_reply.steer) },
    { value: "after_run", label: t(($) => $.preferences.running_agent_reply.after_run) },
  ];

  return (
    <SettingsRow
      anchor="running-agent-reply"
      label={t(($) => $.preferences.running_agent_reply.title)}
      description={t(($) => $.preferences.running_agent_reply.hint)}
      size="select"
    >
      <Select
        items={options}
        value={value}
        onValueChange={(next) => {
          if (next && next !== value) setValue(next as RunningAgentReply);
        }}
      >
        <SelectTrigger
          size="sm"
          className="w-full"
          aria-label={t(($) => $.preferences.running_agent_reply.title)}
        >
          <SelectValue>
            {options.find((option) => option.value === value)?.label}
          </SelectValue>
        </SelectTrigger>
        <SelectContent align="end">
          {options.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}

/**
 * When off, the FAB / overlay never mount and Chat is reachable only from its
 * dedicated tab. A persisted client setting, so it applies immediately.
 */
function FloatingChatRow() {
  const { t } = useT("settings");
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const setEnabled = useChatStore((s) => s.setFloatingChatEnabled);
  return (
    <SettingsRow
      anchor="chat"
      label={t(($) => $.chat.floating_label)}
      description={t(($) => $.chat.floating_hint)}
    >
      <Switch
        checked={enabled}
        onCheckedChange={(checked) => setEnabled(checked)}
        aria-label={t(($) => $.chat.floating_label)}
      />
    </SettingsRow>
  );
}

/**
 * Which fields each create-issue dialog keeps on its toolbar, as one field ×
 * dialog grid. A field toggled off stays reachable from the dialog's ⋯
 * overflow and re-surfaces while it holds a value, so hiding is never
 * destructive. Quick create supports fewer fields; those cells show a dash.
 */
function IssueFieldsMatrix() {
  const { t } = useT("settings");
  const quickFields = useIssueCreateSettingsStore((s) => s.quickCreateFields);
  const setQuickVisible = useIssueCreateSettingsStore(
    (s) => s.setQuickCreateFieldVisible,
  );
  const manualFields = useIssueCreateSettingsStore((s) => s.manualCreateFields);
  const setManualVisible = useIssueCreateSettingsStore(
    (s) => s.setManualCreateFieldVisible,
  );
  const quickLabel = t(($) => $.preferences.issue_fields.quick);
  const manualLabel = t(($) => $.preferences.issue_fields.manual);

  return (
    <SettingsCard>
      <table className="w-full text-body">
        <thead>
          <tr className="border-b border-surface-border bg-muted/20 text-caption font-medium text-muted-foreground">
            <th scope="col" className="px-4 py-2 text-left font-medium">
              {t(($) => $.preferences.issue_fields.field)}
            </th>
            <th scope="col" className="w-32 px-4 py-2 text-center font-medium">
              {quickLabel}
            </th>
            <th scope="col" className="w-32 px-4 py-2 text-center font-medium">
              {manualLabel}
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-surface-border">
          {MANUAL_CREATE_FIELDS.map((field: ManualCreateField) => {
            const fieldLabel = t(($) => $.issue.fields[field]);
            const quickSupported = (QUICK_CREATE_FIELDS as readonly string[]).includes(field);
            return (
              <tr key={field}>
                <th scope="row" className="px-4 py-2.5 text-left font-normal">
                  {fieldLabel}
                </th>
                <td className="px-4 py-2.5 text-center">
                  {quickSupported ? (
                    <Checkbox
                      checked={quickFields.includes(field as QuickCreateField)}
                      onCheckedChange={(checked) =>
                        setQuickVisible(field as QuickCreateField, checked === true)
                      }
                      aria-label={`${fieldLabel} · ${quickLabel}`}
                    />
                  ) : (
                    <span
                      className="text-faint-foreground"
                      aria-label={t(($) => $.preferences.issue_fields.unsupported)}
                      title={t(($) => $.preferences.issue_fields.unsupported)}
                    >
                      —
                    </span>
                  )}
                </td>
                <td className="px-4 py-2.5 text-center">
                  <Checkbox
                    checked={manualFields.includes(field)}
                    onCheckedChange={(checked) =>
                      setManualVisible(field, checked === true)
                    }
                    aria-label={`${fieldLabel} · ${manualLabel}`}
                  />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </SettingsCard>
  );
}

// Base UI rejects "" as an item value, so route the "no preference" state
// through this sentinel and translate at the wire boundary.
const BROWSER_TZ_VALUE = "__browser__";

/** "UTC+8", "UTC-3:30" — the offset right now, for scanning the list. */
function utcOffset(tz: string): string {
  try {
    const part = new Intl.DateTimeFormat("en-US", {
      timeZone: tz,
      timeZoneName: "shortOffset",
    })
      .formatToParts(new Date())
      .find((p) => p.type === "timeZoneName")?.value;
    return part ? part.replace(/^GMT/, "UTC").replace(/^UTC$/, "UTC+0") : "";
  } catch {
    return "";
  }
}

function TimezoneRow() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [open, setOpen] = useState(false);
  const stored = user?.timezone ?? null;
  const browser = browserTimezone();
  const value = stored ?? BROWSER_TZ_VALUE;

  // The full IANA list (~600 zones) so a user needing a non-curated zone is
  // not stuck with the common ones — which is why this is searchable.
  const options = useMemo(
    () =>
      timezoneOptions(stored ?? browser).map((tz) => ({
        tz,
        offset: utcOffset(tz),
      })),
    [stored, browser],
  );

  const handleChange = async (next: string) => {
    setOpen(false);
    if (next === value) return;
    const payload = next === BROWSER_TZ_VALUE ? "" : next;
    try {
      const updated = await api.updateMe({ timezone: payload });
      setUser(updated);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.preferences.timezone.sync_failed),
      );
    }
  };

  const current = stored ?? browser;
  const suffix = stored ? "" : t(($) => $.preferences.timezone.browser_suffix);

  return (
    <SettingsRow
      anchor="timezone"
      label={t(($) => $.preferences.timezone.title)}
      description={t(($) => $.preferences.timezone.hint)}
      size="select-wide"
    >
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.preferences.timezone.title)}
              className="flex h-7 w-full items-center gap-1.5 rounded-md border border-input bg-transparent pl-2.5 pr-2 text-left text-body outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 dark:hover:bg-input/50"
            />
          }
        >
          <span className="min-w-0 flex-1 truncate font-mono text-caption">
            {current}
            {suffix}
          </span>
          <span className="shrink-0 text-caption text-muted-foreground">
            {utcOffset(current)}
          </span>
          <ChevronsUpDown aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 p-0">
          {/* Plain substring matching: fuzzy ranking puts "Antarctica/Vostok"
              next to "Asia/Tokyo" for "tok", which reads as a wrong result. */}
          <Command
            filter={(itemValue, search) =>
              itemValue.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) ? 1 : 0
            }
          >
            <CommandInput placeholder={t(($) => $.preferences.timezone.search)} />
            <CommandList>
              <CommandEmpty>{t(($) => $.preferences.timezone.empty)}</CommandEmpty>
              <CommandItem
                value={`${browser} ${t(($) => $.preferences.timezone.browser_suffix)}`}
                data-checked={value === BROWSER_TZ_VALUE}
                onSelect={() => void handleChange(BROWSER_TZ_VALUE)}
              >
                <span className="min-w-0 flex-1 truncate font-mono text-caption">
                  {browser}
                  {t(($) => $.preferences.timezone.browser_suffix)}
                </span>
                <span className="text-caption text-muted-foreground">{utcOffset(browser)}</span>
              </CommandItem>
              {options.map(({ tz, offset }) => (
                <CommandItem
                  key={tz}
                  value={`${tz} ${offset}`}
                  data-checked={value === tz}
                  onSelect={() => void handleChange(tz)}
                >
                  <span className="min-w-0 flex-1 truncate font-mono text-caption">{tz}</span>
                  <span className="text-caption text-muted-foreground">{offset}</span>
                </CommandItem>
              ))}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </SettingsRow>
  );
}
