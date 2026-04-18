import { create } from "zustand";

/**
 * Window-level transition overlay: pre-workspace flows that are NOT pages
 * inside a tab. Triggered by navigation-adapter interception, zero-workspace
 * auto-redirect, or deep link; rendered above the tab system as a full-window
 * takeover.
 *
 * These flows used to be routes (`/workspaces/new`, `/invite/:id`) but on
 * desktop the URL is invisible to users — routes are an implementation detail
 * of the tab system. Representing transitions as routes meant tabs tried to
 * persist them, TabBar rendered on top, and invite deep-linking had no clean
 * dispatch target. Modeling them as application state removes all three.
 */
export type WindowOverlay =
  | { type: "new-workspace" }
  | { type: "invite"; invitationId: string };

interface WindowOverlayStore {
  overlay: WindowOverlay | null;
  open: (overlay: WindowOverlay) => void;
  close: () => void;
}

export const useWindowOverlayStore = create<WindowOverlayStore>((set) => ({
  overlay: null,
  open: (overlay) => set({ overlay }),
  close: () => set({ overlay: null }),
}));
