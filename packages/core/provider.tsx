"use client";

import { useEffect, useState } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { bindWorkspaceSlugLookup, createQueryClient } from "./query-client";
import type { ReactNode } from "react";

export function QueryProvider({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  // Bind the client React actually kept. Effects never run during server
  // rendering, so account state is never shared between requests. Lookups
  // read the cache lazily, and the workspace list only arrives after mount.
  useEffect(() => bindWorkspaceSlugLookup(queryClient), [queryClient]);
  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  );
}
