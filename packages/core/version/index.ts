"use client";

import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";
import { useConfigStore } from "../config";
import { getClientIdentity } from "../platform/client-identity";
import { compareVersions } from "./compare";

/**
 * Session-scoped dismissal for the server-version-mismatch banner (#5848).
 * Deliberately not persisted: the warning re-appears on the next app start so
 * a stale self-hosted server keeps nudging until it is upgraded.
 */
interface VersionMismatchState {
  dismissed: boolean;
  dismiss: () => void;
}

export const versionMismatchStore = createStore<VersionMismatchState>((set) => ({
  dismissed: false,
  dismiss: () => set({ dismissed: true }),
}));

export interface ServerVersionMismatch {
  /** True only when the app is provably newer than the server and the user
   * has not dismissed the banner this session. */
  show: boolean;
  appVersion: string;
  serverVersion: string;
  dismiss: () => void;
}

/**
 * Compares this client's version (platform identity stamped at boot) with the
 * server version fetched by AuthInitializer into configStore — no extra
 * request. "unknown" (cloud omitting the field, dev builds) never warns, and
 * a newer server never warns either (the client update flow owns that case).
 */
export function useServerVersionMismatch(): ServerVersionMismatch {
  const serverVersion = useConfigStore((state) => state.serverVersion);
  const dismissed = useStore(versionMismatchStore, (state) => state.dismissed);
  const appVersion = getClientIdentity()?.version ?? "";
  const relation = compareVersions(appVersion, serverVersion);
  return {
    show: relation === "app_newer" && !dismissed,
    appVersion,
    serverVersion,
    dismiss: versionMismatchStore.getState().dismiss,
  };
}
