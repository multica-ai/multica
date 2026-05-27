"use client";

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../auth";
import { useLocaleAdapter } from "./adapter-context";
import { SUPPORTED_LOCALES, type SupportedLocale } from "./types";

// Pulls the server-stored `user.language` into the local locale adapter on
// login. Without this, switching device (macOS → Windows, browser → desktop)
// loses the user's language preference: pickLocale only consults the local
// adapter (cookie / localStorage), never user.language.
//
// Mounts inside CoreProvider so it has access to the auth store + locale
// adapter + i18n instance. Renders nothing.
//
// Loop safety: changeLanguage only fires when user.language is a supported
// locale and differs from the active i18n language. The persisted adapter value
// keeps the next app boot aligned with the explicit server-stored preference.
export function UserLocaleSync() {
  const userLanguage = useAuthStore((s) => s.user?.language ?? null);
  const adapter = useLocaleAdapter();
  const { i18n } = useTranslation();

  useEffect(() => {
    if (!userLanguage) return;
    if (!(SUPPORTED_LOCALES as readonly string[]).includes(userLanguage)) {
      return;
    }
    if (userLanguage === i18n.language) return;
    const next = userLanguage as SupportedLocale;
    adapter.persist(next);
    void i18n.changeLanguage(next);
  }, [userLanguage, i18n, i18n.language, adapter]);

  return null;
}
