import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  // Email gate: when true, the server has SMTP/Resend configured and can
  // deliver email notifications. Defaults to false; older servers that omit
  // the field are treated as "email not configured".
  emailConfigured: boolean;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    workspaceCreationDisabled?: boolean;
    emailConfigured?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  emailConfigured: false,
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({ allowSignup, googleClientId = "", workspaceCreationDisabled = false, emailConfigured = false }) =>
    set({ allowSignup, googleClientId, workspaceCreationDisabled, emailConfigured }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
