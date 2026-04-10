"use client";

import { useEffect } from "react";

/**
 * Syncs <html lang> with the active locale.
 * The locale is passed as a prop from the [locale] layout (server component),
 * and also persisted to localStorage so middleware can restore it on re-entry.
 */
export function LocaleSync({ locale }: { locale: string }) {
  useEffect(() => {
    document.documentElement.lang = locale;
    // Persist preference so middleware/cookie can restore locale on next visit
    try {
      localStorage.setItem("multica-locale", locale);
    } catch {
      // localStorage unavailable (e.g. private browsing restrictions)
    }
  }, [locale]);

  return null;
}
