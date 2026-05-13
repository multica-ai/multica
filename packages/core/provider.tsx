"use client";

import { useState } from "react";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
export { useQuery as useCoreQuery } from "@tanstack/react-query";
import { createQueryClient } from "./query-client";
import type { ReactNode } from "react";

export function QueryProvider({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  );
}

export function useCoreQueryClient() {
  return useQueryClient();
}
