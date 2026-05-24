import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  apiBaseUrl: string;
  allowSignup: boolean;
  googleClientId: string;
  setCdnDomain: (domain: string) => void;
  setApiBaseUrl: (url: string) => void;
  setAuthConfig: (config: { allowSignup: boolean; googleClientId?: string }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  apiBaseUrl: "",
  allowSignup: true,
  googleClientId: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setApiBaseUrl: (url) => set({ apiBaseUrl: url }),
  setAuthConfig: ({ allowSignup, googleClientId = "" }) =>
    set({ allowSignup, googleClientId }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
