import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
const VIEWPORT_MENU_BOUNDARY_SELECTOR = '[data-slot="viewport-menu-boundary"]';
const COMPOSER_MENU_WINDOW_FRAME_SELECTOR =
  "[data-workbench-window-id], [data-workspace-node-window-root='true']";

export function resolveComposerPortalTarget(anchor: HTMLElement): Element {
  const boundary = anchor.closest(VIEWPORT_MENU_BOUNDARY_SELECTOR);
  if (boundary?.getAttribute("data-viewport-menu-portal-target") === "body") {
    return getPortalContainer();
  }

  return (
    boundary ??
    anchor.closest(COMPOSER_MENU_WINDOW_FRAME_SELECTOR) ??
    getPortalContainer()
  );
}
