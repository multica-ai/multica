"use client";

import { useLayoutEffect } from "react";

/**
 * Owns the browser tab title for client-resolved dashboard routes.
 *
 * Prefer React 19's hoisted `<title>` so Next's metadata manager and the
 * document stay on the same value. Also assign `document.title` in
 * `useLayoutEffect` so a first paint after a cold issue-link open cannot
 * linger on the root layout default. While mounted, observe the document head
 * and restore the owner title if Next's metadata manager later re-applies the
 * public brand default after hydration.
 */
export function PageTitle({ title }: { title: string }) {
  useLayoutEffect(() => {
    const synchronizeTitle = () => {
      if (title && document.title !== title) document.title = title;
    };

    synchronizeTitle();

    const observer = new MutationObserver(synchronizeTitle);
    observer.observe(document.head, {
      childList: true,
      subtree: true,
      characterData: true,
    });
    return () => observer.disconnect();
  }, [title]);

  return <title>{title}</title>;
}
