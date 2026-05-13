import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  larkAuthEnabled: boolean;
  larkAppId: string;
  larkAuthorizeUrl: string;
  releaseRepository: string;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    larkAuthEnabled?: boolean;
    larkAppId?: string;
    larkAuthorizeUrl?: string;
    releaseRepository?: string;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  larkAuthEnabled: false,
  larkAppId: "",
  larkAuthorizeUrl: "",
  releaseRepository: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({
    allowSignup,
    googleClientId = "",
    larkAuthEnabled = false,
    larkAppId = "",
    larkAuthorizeUrl = "",
    releaseRepository = "",
  }) =>
    set({
      allowSignup,
      googleClientId,
      larkAuthEnabled,
      larkAppId,
      larkAuthorizeUrl,
      releaseRepository,
    }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
