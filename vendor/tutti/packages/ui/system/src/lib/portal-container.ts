/**
 * Where GUI overlays portal to. The GUI shares its document with the app, and
 * its styles only apply under an element the host marks up, so document.body
 * would leave overlays unstyled.
 */
const PORTAL_ROOT_ATTRIBUTE = "data-tutti-portal-root";

export function getPortalContainer(): HTMLElement {
  const existing = document.querySelector<HTMLElement>(`[${PORTAL_ROOT_ATTRIBUTE}]`);
  if (existing) return existing;
  const root = document.createElement("div");
  root.setAttribute(PORTAL_ROOT_ATTRIBUTE, "true");
  document.body.append(root);
  return root;
}
