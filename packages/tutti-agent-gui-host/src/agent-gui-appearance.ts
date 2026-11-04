import { getPortalContainer } from "@tutti-os/ui-system/portal-container";

export const AGENT_GUI_ROOT_CLASS = "multica-agent-gui";

/** Applies what the GUI's styles read from their root. */
export function applyAgentGuiAppearance(element: HTMLElement, appearance: { theme: string; locale: string; sidePane?: boolean }) {
  element.classList.add(AGENT_GUI_ROOT_CLASS);
  element.dataset.multicaPresentation = "conversation";
  element.dataset.sidePane = String(appearance.sidePane === true);
  element.dataset.theme = appearance.theme;
  element.classList.toggle("dark", appearance.theme === "dark");
  element.lang = appearance.locale;
}

/**
 * Overlays share one container, and code outside an instance reads the GUI's
 * theme there. Nothing goes on <html>: the app's own tokens respond to data-theme.
 */
export function syncAgentGuiDocument(appearance: { theme: string; locale: string }) {
  applyAgentGuiAppearance(getPortalContainer(), appearance);
}
