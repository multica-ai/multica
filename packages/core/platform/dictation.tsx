"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { DictationAdapter } from "../types/dictation";

const DictationContext = createContext<DictationAdapter | undefined>(undefined);

/** A provided host adapter is authoritative: consumers must not fall back to
 * paid/server transcription when it rejects a request. */
export function DictationProvider({
  adapter,
  children,
}: {
  adapter?: DictationAdapter;
  children: ReactNode;
}) {
  return (
    <DictationContext.Provider value={adapter}>
      {children}
    </DictationContext.Provider>
  );
}

export function useDictationAdapter(): DictationAdapter | undefined {
  return useContext(DictationContext);
}
