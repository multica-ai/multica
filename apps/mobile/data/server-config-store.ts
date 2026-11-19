import { create } from "zustand";
import { normalizeServerConfig, type ServerConfig } from "@/lib/server-url";

export const CLOUD_SERVER_CONFIG: ServerConfig = {
  apiUrl: "https://api.multica.ai",
  webUrl: "https://multica.ai",
};

// Environment values only choose the initial install's defaults. A saved
// preference always wins, including after an application upgrade.
export const DEFAULT_SERVER_CONFIG: ServerConfig = {
  apiUrl: (process.env.EXPO_PUBLIC_API_URL ?? CLOUD_SERVER_CONFIG.apiUrl).replace(/\/+$/, ""),
  webUrl: (process.env.EXPO_PUBLIC_WEB_URL ?? CLOUD_SERVER_CONFIG.webUrl).replace(/\/+$/, ""),
};

export const SERVER_CONFIG_KEY = "multica_server_config";

interface ServerConfigState {
  config: ServerConfig;
  initialized: boolean;
  switching: boolean;
  error: boolean;
}

export const useServerConfigStore = create<ServerConfigState>(() => ({
  config: DEFAULT_SERVER_CONFIG,
  initialized: false,
  switching: false,
  error: false,
}));

let initialization: Promise<void> | null = null;

export async function initializeServerConfig(): Promise<void> {
  if (useServerConfigStore.getState().initialized) return;
  if (!initialization) {
    initialization = (async () => {
      // Lazy import keeps URL resolvers usable in the pure helper test lane.
      const storage = await import("expo-secure-store");
      const raw = await storage.getItemAsync(SERVER_CONFIG_KEY);
      let config = DEFAULT_SERVER_CONFIG;
      if (raw !== null) {
        const parsed: unknown = JSON.parse(raw);
        if (!parsed || typeof parsed !== "object" ||
            !("apiUrl" in parsed) || typeof parsed.apiUrl !== "string" ||
            !("webUrl" in parsed) || typeof parsed.webUrl !== "string") {
          throw new Error("Invalid saved server configuration");
        }
        config = normalizeServerConfig({ apiUrl: parsed.apiUrl, webUrl: parsed.webUrl });
      }
      useServerConfigStore.setState({ config, initialized: true, error: false });
    })().catch((error: unknown) => {
      // Fail closed: never restore a self-hosted credential against Cloud
      // because its saved server preference could not be read.
      useServerConfigStore.setState({ error: true });
      initialization = null;
      throw error;
    });
  }
  return initialization;
}

/** Caller must first clear the old session; the pair is one atomic write. */
export async function persistServerConfig(config: ServerConfig): Promise<void> {
  const storage = await import("expo-secure-store");
  await storage.setItemAsync(SERVER_CONFIG_KEY, JSON.stringify(config));
  useServerConfigStore.setState({ config, initialized: true, error: false });
}

export function getServerConfig(): ServerConfig {
  return useServerConfigStore.getState().config;
}
