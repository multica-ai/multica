"use client";

import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { DEFAULT_LOCALE, SUPPORTED_LOCALES, type SupportedLocale } from "@multica/core/i18n";
import { useLocaleAdapter } from "@multica/core/i18n/react";
import { useT } from "../i18n";

/** The UI language options, the current one, and the switch shared by Settings and the account menu. */
export function useLanguageSwitch() {
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

  const options: { value: SupportedLocale; label: string }[] = [
    { value: "en", label: t(($) => $.preferences.language.english) },
    { value: "zh-Hans", label: t(($) => $.preferences.language.chinese) },
    { value: "zh-Hant", label: t(($) => $.preferences.language.chinese_traditional) },
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
  const changeLanguage = async (next: SupportedLocale) => {
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

  return { options, currentLocale, changeLanguage };
}
