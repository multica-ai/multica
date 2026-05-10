import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  // singleUser mirrors the backend MULTICA_SINGLE_USER flag. When true the
  // web app hides the login UI, the marketing landing page, and the
  // "Download Desktop" CTA — every visitor is the local user.
  singleUser: boolean;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    singleUser?: boolean;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  singleUser: false,
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({ allowSignup, googleClientId = "", singleUser = false }) =>
    set({ allowSignup, googleClientId, singleUser }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
