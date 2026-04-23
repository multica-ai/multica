import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

export interface OAuthProviderRuntimeConfig {
  clientId: string;
  authorizeUrl: string;
  /** Relative path the provider redirects back to; must be echoed on token exchange. */
  callbackPath: string;
  scope: string;
  extraAuthParams?: Record<string, string>;
}

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  oauthProviders: Record<string, OAuthProviderRuntimeConfig>;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    oauthProviders?: Record<string, OAuthProviderRuntimeConfig>;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  oauthProviders: {},
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({ allowSignup, oauthProviders = {} }) =>
    set({ allowSignup, oauthProviders }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
