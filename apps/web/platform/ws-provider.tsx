"use client";

import { WSProvider } from "@multica/core/realtime";
import { useAuthStore } from "./auth";
import { useWorkspaceStore } from "./workspace";
import { webStorage } from "./storage";
import { toast } from "sonner";

const WS_URL = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws";

export function WebWSProvider({ children }: { children: React.ReactNode }) {
  return (
    <WSProvider
      wsUrl={WS_URL}
      authStore={useAuthStore}
      workspaceStore={useWorkspaceStore}
      storage={webStorage}
      onToast={(message, type) => {
        if (type === "error") toast.error(message);
        else toast.info(message);
      }}
    >
      {children}
    </WSProvider>
  );
}
