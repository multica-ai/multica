import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  oidcIssuerURL: string;
  oidcClientID: string;
  oidcAuthorizationEndpoint: string;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    oidcIssuerURL?: string;
    oidcClientID?: string;
    oidcAuthorizationEndpoint?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  oidcIssuerURL: "",
  oidcClientID: "",
  oidcAuthorizationEndpoint: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({
    allowSignup,
    googleClientId = "",
    oidcIssuerURL = "",
    oidcClientID = "",
    oidcAuthorizationEndpoint = "",
  }) =>
    set({
      allowSignup,
      googleClientId,
      oidcIssuerURL,
      oidcClientID,
      oidcAuthorizationEndpoint,
    }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
