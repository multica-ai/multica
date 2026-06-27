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
  // True when the server has GitLab OAuth configured and the login button
  // should be shown. Defaults to false so older servers without the field
  // don't surface a non-functional button.
  gitLabEnabled: boolean;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  setCdnConfig: (config: { cdnDomain: string; cdnSigned?: boolean }) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    gitLabEnabled?: boolean;
    workspaceCreationDisabled?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  cdnSigned: false,
  allowSignup: true,
  googleClientId: "",
  gitLabEnabled: false,
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  setCdnConfig: ({ cdnDomain, cdnSigned = false }) => set({ cdnDomain, cdnSigned }),
  setAuthConfig: ({
    allowSignup,
    googleClientId = "",
    gitLabEnabled = false,
    workspaceCreationDisabled = false,
  }) => set({ allowSignup, googleClientId, gitLabEnabled, workspaceCreationDisabled }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
