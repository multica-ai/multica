"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { LocaleAdapter } from "./types";

const LocaleAdapterContext = createContext<LocaleAdapter | null>(null);

export function LocaleAdapterProvider({
  adapter,
  children,
}: {
  adapter: LocaleAdapter;
  children: ReactNode;
}) {
  return (
    <LocaleAdapterContext.Provider value={adapter}>
      {children}
    </LocaleAdapterContext.Provider>
  );
}

export function useLocaleAdapter(): LocaleAdapter {
  const ctx = useContext(LocaleAdapterContext);
  if (!ctx) {
    throw new Error(
      "useLocaleAdapter must be used within <LocaleAdapterProvider>",
    );
  }
  return ctx;
}
