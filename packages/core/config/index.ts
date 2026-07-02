import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  // True when cdnDomain serves private content via time-bounded signed URLs
  // (CloudFront signing enabled server-side). Renderers must not treat a raw
  // storage URL on that domain as a loadable media source (MUL-3254).
  cdnSigned: boolean;
  allowSignup: boolean;
  googleClientId: string;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  // Desktop on-hang CPU profiling feature flag (MUL-3738). Defaults false;
  // unknown / older servers behave as off. Read by the desktop diagnostics
  // bridge, which pushes it to the Electron main process.
  cpuProfileEnabled: boolean;
  setCdnConfig: (config: { cdnDomain: string; cdnSigned?: boolean }) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    workspaceCreationDisabled?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
  setDiagnosticsConfig: (config: { cpuProfileEnabled?: boolean }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  cdnSigned: false,
  allowSignup: true,
  googleClientId: "",
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  cpuProfileEnabled: false,
  setCdnConfig: ({ cdnDomain, cdnSigned = false }) => set({ cdnDomain, cdnSigned }),
  setAuthConfig: ({ allowSignup, googleClientId = "", workspaceCreationDisabled = false }) =>
    set({ allowSignup, googleClientId, workspaceCreationDisabled }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
  setDiagnosticsConfig: ({ cpuProfileEnabled = false }) => set({ cpuProfileEnabled }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
