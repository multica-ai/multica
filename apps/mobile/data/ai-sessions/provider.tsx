import { createContext, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from "react";
import { AppState, Platform } from "react-native";
import { requireOptionalNativeModule } from "expo";
import NetInfo from "@react-native-community/netinfo";
import { api } from "@/data/api";
import { useAuthStore } from "@/data/auth-store";
import { onSessionInvalidated } from "@/data/session-epoch";
import type { DeviceEndpoint } from "./client";
import { clearDeviceFiles } from "./files";
import { EmbeddedDaemonManager, networkSignature, type NativeDaemon } from "./daemon-manager";

const native = Platform.OS === "ios" ? requireOptionalNativeModule<NativeDaemon>("MulticaDeviceLink") : null;
const manager = new EmbeddedDaemonManager(native);
const Context = createContext<{ endpoint: DeviceEndpoint | null; error: string | null; retry(): void }>({ endpoint: null, error: null, retry() {} });

/** App-owned daemon: starts after auth restoration, independently of pages. */
export function AIDeviceProvider({ children }: { children: ReactNode }) {
  const userId = useAuthStore((s) => s.user?.id);
  const token = useSyncExternalStore((listener) => api.onTokenChange(listener), () => api.getToken());
  const state = useSyncExternalStore(manager.subscribe, manager.snapshot);
  const configuration = useMemo(() => userId && token ? {
    registryUrl: process.env.EXPO_PUBLIC_P2P_SERVER_URL ?? "http://101.33.221.22:8090",
    apiUrl: process.env.EXPO_PUBLIC_API_URL ?? "", token, userId, deviceName: "Multica iOS",
  } : null, [userId, token]);
  useEffect(() => {
    void manager.setForeground(AppState.currentState !== "background");
    const subscription = AppState.addEventListener("change", (value) => { if (value !== "inactive") void manager.setForeground(value === "active"); });
    return () => subscription.remove();
  }, []);
  useEffect(() => onSessionInvalidated(() => { clearDeviceFiles(); void manager.stop(); }), []);
  useEffect(() => {
    let previous: string | undefined;
    return NetInfo.addEventListener((value) => {
      const signature = networkSignature(value);
      if (previous !== undefined && previous !== signature) void manager.networkChanged();
      previous = signature;
      if (value.isConnected && AppState.currentState === "active" && manager.snapshot().status === "failed") void manager.restart();
    });
  }, []);
  useEffect(() => { void manager.configure(configuration); }, [configuration]);
  useEffect(() => () => { void manager.stop(); }, []);
  return <Context.Provider value={{ endpoint: state.endpoint, error: state.error, retry: () => { void manager.restart(); } }}>{children}</Context.Provider>;
}
export const useAIDeviceLink = () => useContext(Context);
