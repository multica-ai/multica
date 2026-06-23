import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  serverUrl: string;
  cliServerUrl: string;
  allowSignup: boolean;
  googleClientId: string;
  appEnv: string;
  casdoorEnabled: boolean;
  casdoorLoginUrl: string;
  setCdnDomain: (domain: string) => void;
  setServerUrl: (url: string) => void;
  setCliServerUrl: (url: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    appEnv?: string;
    casdoorEnabled?: boolean;
    casdoorLoginUrl?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  serverUrl: "",
  cliServerUrl: "",
  allowSignup: true,
  googleClientId: "",
  appEnv: "",
  casdoorEnabled: false,
  casdoorLoginUrl: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setServerUrl: (url) => set({ serverUrl: url }),
  setCliServerUrl: (url) => set({ cliServerUrl: url }),
  setAuthConfig: ({ allowSignup, googleClientId = "", appEnv = "", casdoorEnabled = false, casdoorLoginUrl = "" }) =>
    set({ allowSignup, googleClientId, appEnv, casdoorEnabled, casdoorLoginUrl }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
