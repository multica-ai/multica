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
// Loop safety: language switching only fires when user.language is a supported
// locale AND differs from the active i18n.language. After changeLanguage,
// locales match and the effect no-ops.
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
    adapter.persist(userLanguage as SupportedLocale);
    void i18n.changeLanguage(userLanguage);
  }, [userLanguage, i18n.language, adapter]);

  return null;
}
