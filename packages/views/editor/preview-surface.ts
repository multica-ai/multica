"use client";

import { createContext, use } from "react";

/**
 * Whether the preview stage is mounted in the docked side pane (MUL-10014)
 * rather than the full-window viewer or the new-tab page. Those two sit on a
 * black ground and dress their stage text in `dark`; the pane sits in the
 * page, in the app's own theme, with no gutters around its content.
 */
export const PreviewPaneSurfaceContext = createContext(false);

export function useInPreviewPane(): boolean {
  return use(PreviewPaneSurfaceContext);
}

/** The theme class for text set directly on the stage. */
export function useStageThemeClass(): string {
  return useInPreviewPane() ? "" : "dark";
}
