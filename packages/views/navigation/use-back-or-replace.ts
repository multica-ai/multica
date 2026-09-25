"use client";

import { useCallback } from "react";
import { useNavigation } from "./context";

/**
 * Leave a page whose subject no longer exists, returning the user to wherever
 * they came from.
 *
 * Deleting an issue from its detail page is the canonical case: the user may
 * have opened it from My Issues, a project's list, a search result or a pin,
 * and every one of those is a better destination than a hardcoded workspace
 * list. Going back is the only thing that knows which — no caller has to
 * thread a "source view" through the URL or a store.
 *
 * `fallback` covers the case where there is nothing to go back to: a shared
 * link opened cold, a new tab, a pasted URL. Stepping back from those would
 * leave Multica entirely, so we navigate to `fallback` instead.
 *
 * It also covers every platform that cannot answer the question — the desktop
 * issue window, test stubs, and browsers with no Navigation API. That branch
 * is the behaviour those surfaces had before this hook existed, so a platform
 * going quiet costs the user a better destination and nothing more. Only ever
 * answer `canGoBack` from something that knows; a hopeful guess here is what
 * puts a user outside the app.
 *
 * Never `push`: the page we are leaving is dead, so its URL must not be left
 * on the back stack for the back button to land on a 404. Stepping back still
 * leaves it ahead of us in forward history — that is the browser's own
 * contract, and reachable only by an explicit Forward.
 */
export function useBackOrReplace(): (fallback: string) => void {
  const { back, replace, canGoBack } = useNavigation();
  return useCallback(
    (fallback: string) => {
      if (canGoBack?.() === true) back();
      else replace(fallback);
    },
    [back, replace, canGoBack],
  );
}
