"use client";

import { useState } from "react";
import { PersistQueryClientProvider } from "@tanstack/react-query-persist-client";
import { createAsyncStoragePersister } from "@tanstack/query-async-storage-persister";
import { get, set, del } from "idb-keyval";
import { createQueryClient } from "./query-client";
import type { ReactNode } from "react";

export function QueryProvider({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  
  const [persister] = useState(() => 
    createAsyncStoragePersister({
      storage: {
        getItem: async (key) => await get(key),
        setItem: async (key, value) => await set(key, value),
        removeItem: async (key) => await del(key),
      },
    })
  );

  return (
    <PersistQueryClientProvider 
      client={queryClient} 
      persistOptions={{ 
        persister,
        maxAge: 1000 * 60 * 60 * 24 * 7 // 7 days
      }}
    >
      {children}
    </PersistQueryClientProvider>
  );
}
