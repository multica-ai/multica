"use client";

/**
 * Scroll root for near-viewport rich-block mounting (MUL-4922).
 *
 * IntersectionObserver clips against its `root`. Left unset it uses the browser
 * viewport, and `rootMargin` then expands the *viewport* box — which says
 * nothing useful about a block sitting inside a nested scroll container. Chat's
 * list scrolls in its own element (`Virtuoso customScrollParent`), so with the
 * default root a message only counts as "near" once it is already visible, and
 * the whole point of preloading is lost.
 *
 * Surfaces that scroll inside their own element publish it here. Surfaces that
 * scroll with the page (Issue description, Comment) publish nothing and keep the
 * viewport root, which is correct for them.
 */

import { createContext, useContext, type ReactNode } from "react";

const RichContentScrollRootContext = createContext<HTMLElement | null>(null);

export function RichContentScrollRootProvider({
  scrollRoot,
  children,
}: {
  scrollRoot: HTMLElement | null;
  children: ReactNode;
}) {
  return (
    <RichContentScrollRootContext.Provider value={scrollRoot}>
      {children}
    </RichContentScrollRootContext.Provider>
  );
}

/**
 * The element rich blocks should measure "near-viewport" against, or null to
 * use the browser viewport.
 */
export function useRichContentScrollRoot(): HTMLElement | null {
  return useContext(RichContentScrollRootContext);
}
