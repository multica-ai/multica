import { create } from "zustand";

/**
 * The pathname of the page the router has actually committed. The tab
 * session URL moves first and the router renders the new page in a
 * transition, so for a few frames the session already points at the next
 * page while the previous one is still on screen. Shell chrome that has to
 * line up with the page (the space panel, list columns) reads this instead.
 */
interface RenderedRouteStore {
  pathname: string | null;
  setPathname: (pathname: string) => void;
}

export const useRenderedRouteStore = create<RenderedRouteStore>((set) => ({
  pathname: null,
  setPathname: (pathname) => set({ pathname }),
}));
