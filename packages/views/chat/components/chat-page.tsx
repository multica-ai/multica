"use client";

import { useEffect } from "react";
import { useChatStore } from "@multica/core/chat";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../../navigation";

const MOBILE_BREAKPOINT = 768;

// Mobile treats /chat as a real route — peer of /inbox and /issues — so the
// bottom nav can use the same tap-to-navigate model across all three tabs.
// The actual chat surface still lives in the dashboard layout's `extra` slot
// (so it survives navigation + WS reconnects) and reads route presence to
// decide whether to render. This page only needs to exist so Next.js routes
// /chat to something instead of 404'ing.
//
// On desktop /chat has no meaning — chat is a floating window. Anyone landing
// here (deep link, refresh after a mobile→desktop session swap, etc.) gets
// the floating window opened and is bounced to /issues so the URL doesn't
// linger on a blank route. We probe window.innerWidth directly inside the
// effect rather than calling useIsMobile, because that hook returns `false`
// on first render (its internal state initialises to undefined and only
// flips after the first effect tick) — using it here would redirect every
// real mobile device too, racing the bottom-nav tap.
export function ChatPage() {
  const setOpen = useChatStore((s) => s.setOpen);
  const { replace } = useNavigation();
  const slug = useWorkspaceSlug();

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (window.innerWidth < MOBILE_BREAKPOINT) return;
    if (!slug) return;
    setOpen(true);
    replace(paths.workspace(slug).issues());
  }, [slug, setOpen, replace]);

  return null;
}
