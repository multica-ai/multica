import { createContext, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocalSearchParams } from "expo-router";
import { useAuthStore } from "@/data/auth-store";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { AISessionClient, aiKeys } from "./client";
import { useAIDeviceLink } from "./provider";
import { devicesSchema, readResponse } from "./schemas";

export function useAIDevices() {
  const { endpoint } = useAIDeviceLink();
  const userId = useAuthStore((s) => s.user?.id);
  return useQuery({ queryKey: aiKeys.devices(userId ?? ""), enabled: !!endpoint && !!userId, retry: false, refetchInterval: 5000,
    queryFn: async ({ signal }) => {
      const controller = new AbortController();
      const abort = () => controller.abort();
      signal.addEventListener("abort", abort);
      if (signal.aborted) controller.abort();
      const timeout = setTimeout(abort, 15000);
      try {
        const response = await fetch(`${endpoint!.url}/devices`, { headers: { Authorization: `Bearer ${endpoint!.token}` }, signal: controller.signal });
        if (!response.ok) throw new Error("connection_failed");
        return readResponse(await response.json(), devicesSchema, "mobile.devices").devices;
      } finally { clearTimeout(timeout); signal.removeEventListener("abort", abort); }
    },
  });
}

const Context = createContext<AISessionClient | null>(null);
// React Native's implementation accepts upgrade headers (unlike browsers).
const NativeSocket = WebSocket as unknown as { new(url: string, protocols: string[], options: { headers: Record<string, string> }): WebSocket };

export function AIConnectionProvider({ children }: { children: ReactNode }) {
  const { endpoint } = useAIDeviceLink();
  const { userId, workspaceId, deviceId: device } = useAISessionScope();
  const qc = useQueryClient();
  const client = useMemo(() => endpoint && userId && workspaceId && device ? new AISessionClient(endpoint, { userId, workspaceId, deviceId: device }, qc,
    (url, headers) => new NativeSocket(url, [], { headers })) : null, [endpoint, userId, workspaceId, device, qc]);
  useEffect(() => { void client?.connect(); return () => client?.close(); }, [client]);
  return <Context.Provider value={client}>{children}</Context.Provider>;
}
export function useAIConnection() {
  const client = useContext(Context);
  const connected = useSyncExternalStore((listener) => client?.onConnection(listener) ?? (() => {}), () => client?.connected ?? false);
  return { client, connected };
}
export function useAISubscriptionRejected(sessionId: string) {
  const client = useContext(Context);
  return useSyncExternalStore((listener) => client?.onConnection(listener) ?? (() => {}), () => client?.subscriptionRejected(sessionId) ?? false);
}

export function useAISessionScope() {
  const { device, workspace } = useLocalSearchParams<{ device: string; workspace: string }>();
  const userId = useAuthStore((s) => s.user?.id) ?? "";
  const { data: workspaces } = useQuery(workspaceListOptions());
  const workspaceId = workspaces?.find((entry) => entry.slug === workspace)?.id ?? "";
  return { userId, workspaceId, deviceId: device };
}
