"use client";

import { useEffect, useState } from "react";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";

/**
 * Session tabs (the in-app document strip) only exist above the compact
 * breakpoint. Until the viewport is known, treat them as off so a phone
 * first-paint cannot flash a tab bar. See docs/adr/0001-web-session-tabs.md.
 */
export function useSessionTabsEnabled(): boolean {
  const compact = useIsCompact();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    setReady(true);
  }, []);
  return ready && !compact;
}
