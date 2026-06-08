"use client";

import * as React from "react";

/**
 * Opt-in container for popup portals (DropdownMenu, Popover, Dialog, HoverCard,
 * Tooltip, …).
 *
 * Defaults to `undefined`, so Base UI portals popups to `document.body` exactly
 * as before — production behavior is unchanged. An embedded surface that lives
 * inside a CSS `transform` (e.g. the landing page's scaled product demo) can
 * provide a node inside its own transformed box, so popups portal there and
 * inherit the same scale instead of rendering at 1:1 over the page.
 *
 * A ref is accepted (resolved lazily by Base UI), so the provider can point at
 * a node that mounts in the same render.
 */
export type PortalContainer =
  | HTMLElement
  | React.RefObject<HTMLElement | null>
  | null
  | undefined;

const PortalContainerContext = React.createContext<PortalContainer>(undefined);

export function PortalContainerProvider({
  container,
  children,
}: {
  container: PortalContainer;
  children: React.ReactNode;
}) {
  return (
    <PortalContainerContext.Provider value={container}>
      {children}
    </PortalContainerContext.Provider>
  );
}

export function usePortalContainer(): PortalContainer {
  return React.useContext(PortalContainerContext);
}
