"use client";

import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { getAppQueryClient } from "./query-client";

export function QueryProvider({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={getAppQueryClient()}>
      {children}
    </QueryClientProvider>
  );
}
