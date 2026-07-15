"use client";

import { useMemo } from "react";
import { toast } from "sonner";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import {
  DEFAULT_LOCALE,
  SUPPORTED_LOCALES,
  type SupportedLocale,
} from "@multica/core/i18n";
import { useLocaleAdapter } from "@multica/core/i18n/react";
import { useAuthStore } from "@multica/core/auth";
import {
  useCommentComposerStore,
  useIssueLinkStore,
} from "@multica/core/issues/stores";
import { api } from "@multica/core/api";
import { browserTimezone, timezoneOptions } from "../../common/timezone-select";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

export function PreferencesTab() {
  const { theme, setTheme } = useTheme();
  const { t, i18n } = useT("settings");
  const localeAdapter = useLocaleAdapter();
  const user = useAuthStore((s) => s.user);

  // i18next.language can be a region-tagged BCP-47 string (e.g. "en-US",
  // "zh-Hans-CN") returned by intl-localematcher. Normalize to a supported
  // locale before comparing — otherwise the radio shows neither option active.
  const currentLocale: SupportedLocale = SUPPORTED_LOCALES.includes(
    i18n.language as SupportedLocale,
  )
    ? (i18n.language as SupportedLocale)
    : DEFAULT_LOCALE;

  const themeOptions = [
    { value: "light" as const, label: t(($) => $.preferences.theme.light) },
    { value: "dark" as const, label: t(($) => $.preferences.theme.dark) },
    { value: "system" as const, label: t(($) => $.preferences.theme.system) },
  ];

  const languageOptions: { value: SupportedLocale; label: string }[] = [
    { value: "en", label: t(($) => $.preferences.language.english) },
    { value: "zh-Hans", label: t(($) => $.preferences.language.chinese) },
    { value: "ko", label: t(($) => $.preferences.language.korean) },
    { value: "ja", label: t(($) => $.preferences.language.japanese) },
  ];

  // Persist locally → sync to user.language → reload. Reload (vs in-place
  // changeLanguage) avoids hydration mismatch and is the i18next-recommended
  // pattern for App Router.
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
    toast.success(t(($) => $.auto_save.toast_saved), {
      id: "settings-auto-save",
    });
    // Keep the confirmation visible before the locale reload replaces the UI.
    setTimeout(() => window.location.reload(), 900);
  };

  return (
    <SettingsTab title={t(($) => $.page.tabs.preferences)}>
      <SettingsSection title={t(($) => $.preferences.general_title)}>
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.preferences.theme.title)}
            size="select"
          >
            <Select
              items={themeOptions}
              value={theme}
              onValueChange={(next) => {
                if (!next || next === theme) return;
                setTheme(next as (typeof themeOptions)[number]["value"]);
                toast.success(t(($) => $.auto_save.toast_saved), {
                  id: "settings-auto-save",
                });
              }}
            >
              <SelectTrigger
                size="sm"
                className="w-full"
                aria-label={t(($) => $.preferences.theme.title)}
              >
                <SelectValue>
                  {themeOptions.find((option) => option.value === theme)?.label}
                </SelectValue>
              </SelectTrigger>
              <SelectContent align="end">
                {themeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SettingsRow>

          <SettingsRow
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

          <TimezoneRow />

          <StickyCommentBarRow />

          <IssueLinkNewTabRow />
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

function StickyCommentBarRow() {
  const { t } = useT("settings");
  const sticky = useCommentComposerStore((s) => s.sticky);
  const toggleSticky = useCommentComposerStore((s) => s.toggleSticky);

  return (
    <SettingsRow
      label={t(($) => $.preferences.sticky_comment_bar.title)}
      description={t(($) => $.preferences.sticky_comment_bar.hint)}
    >
      <Switch
        checked={sticky}
        onCheckedChange={() => {
          toggleSticky();
          toast.success(t(($) => $.auto_save.toast_saved), {
            id: "settings-auto-save",
          });
        }}
        aria-label={t(($) => $.preferences.sticky_comment_bar.title)}
      />
    </SettingsRow>
  );
}

function IssueLinkNewTabRow() {
  const { t } = useT("settings");
  const openInNewTab = useIssueLinkStore((s) => s.openInNewTab);
  const setOpenInNewTab = useIssueLinkStore((s) => s.setOpenInNewTab);

  return (
    <SettingsRow
      label={t(($) => $.preferences.issue_link_new_tab.title)}
      description={t(($) => $.preferences.issue_link_new_tab.hint)}
    >
      <Switch
        checked={openInNewTab}
        onCheckedChange={(checked) => {
          setOpenInNewTab(checked === true);
          toast.success(t(($) => $.auto_save.toast_saved), {
            id: "settings-auto-save",
          });
        }}
        aria-label={t(($) => $.preferences.issue_link_new_tab.title)}
      />
    </SettingsRow>
  );
}

// Base UI rejects "" as a SelectItem value, so route the "no preference"
// state through this sentinel and translate at the wire boundary.
const BROWSER_TZ_VALUE = "__browser__";

function TimezoneRow() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const stored = user?.timezone ?? null;
  const browser = browserTimezone();
  const value = stored ?? BROWSER_TZ_VALUE;

  // Full IANA list (from timezoneOptions in common/timezone-select) so a
  // user needing a non-curated zone isn't stuck with ~18 common ones.
  // Memoized — timezoneOptions enumerates ~600 IANA zones per call.
  const options = useMemo(
    () => timezoneOptions(stored ?? browser),
    [stored, browser],
  );

  const handleChange = async (next: string) => {
    if (next === value) return;
    const payload = next === BROWSER_TZ_VALUE ? "" : next;
    try {
      const updated = await api.updateMe({ timezone: payload });
      setUser(updated);
      toast.success(t(($) => $.auto_save.toast_saved), {
        id: "settings-auto-save",
      });
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.preferences.timezone.sync_failed),
      );
    }
  };

  const formatTZLabel = (tz: string) => {
    if (tz === BROWSER_TZ_VALUE) {
      return `${browser}${t(($) => $.preferences.timezone.browser_suffix)}`;
    }
    return tz;
  };

  return (
    <SettingsRow
      label={t(($) => $.preferences.timezone.title)}
      description={t(($) => $.preferences.timezone.hint)}
      size="select-wide"
    >
      <Select
        items={[
          { value: BROWSER_TZ_VALUE, label: formatTZLabel(BROWSER_TZ_VALUE) },
          ...options.map((timezone) => ({
            value: timezone,
            label: formatTZLabel(timezone),
          })),
        ]}
        value={value}
        onValueChange={(next) => {
          if (next) void handleChange(next);
        }}
      >
        <SelectTrigger
          size="sm"
          className="w-full font-mono text-xs"
          aria-label={t(($) => $.preferences.timezone.title)}
        >
          <SelectValue>{formatTZLabel(value)}</SelectValue>
        </SelectTrigger>
        <SelectContent align="end" className="max-h-72">
          <SelectItem value={BROWSER_TZ_VALUE} className="font-mono text-xs">
            {formatTZLabel(BROWSER_TZ_VALUE)}
          </SelectItem>
          {options.map((timezone) => (
            <SelectItem key={timezone} value={timezone} className="font-mono text-xs">
              {formatTZLabel(timezone)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </SettingsRow>
  );
}
