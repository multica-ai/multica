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
// Loop safety: reload only fires when user.language is a supported locale AND
// differs from the active i18n.language. After reload, pickLocale reads the
// freshly-persisted value from the adapter, locales match, effect no-ops.
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

    // A desktop build can be installed before its hosted server has learned
    // the new zh-Hant value. In that rollout window PATCH /api/me fails, but
    // Settings has already persisted the member's explicit device choice.
    // Do not immediately replace that choice with the server's older locale
    // on reload. Once the server accepts zh-Hant, both values match and this
    // compatibility guard becomes inert.
    if (adapter.getUserChoice() === "zh-Hant") return;

    adapter.persist(userLanguage as SupportedLocale);
    if (typeof window !== "undefined") window.location.reload();
  }, [userLanguage, i18n.language, adapter]);

  return null;
}
