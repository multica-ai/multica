import React from "react";
import { SafeAreaProvider } from "@/lib/safe-area";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/data/query-client";
import { AppShell } from "@/src/app-shell";

export default function App() {
  return (
    <SafeAreaProvider>
      <QueryClientProvider client={queryClient}>
        <AppShell />
      </QueryClientProvider>
    </SafeAreaProvider>
  );
}
