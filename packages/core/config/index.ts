import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";
import type { OAuthProviderPublicConfig } from "../api/client";

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  oauthProviders: OAuthProviderPublicConfig[];
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    oauthProviders?: OAuthProviderPublicConfig[];
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  oauthProviders: [],
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({ allowSignup, googleClientId = "", oauthProviders = [] }) =>
    set({ allowSignup, googleClientId, oauthProviders }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
