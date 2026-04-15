import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

interface ConfigState {
  cdnDomain: string;
  setCdnDomain: (domain: string) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
