// FIR-2037: renderer↔main bridge for the personal browser (cerebro).
//
// Two startup responsibilities, both flag-guarded so a flag-off build is inert:
//
//   1. Ensure the loopback agent-control server (and its `~/.multica` sidecar)
//      is up the moment the app is in a workspace with the `cerebro_browser`
//      flag on — NOT only after the human opens the Browser tab. Without this
//      an agent could never open the tab in the first place, because the
//      sidecar the CLI reads would not yet exist.
//
//   2. Listen for the main process asking us to open & focus the Browser tab
//      (fired when an agent runs `multica cerebro-browser open`). We navigate
//      to the workspace-scoped Browser route via the navigation adapter, which
//      handles cross-workspace switches; the page mount then shows the pane.
//
// Mounted once from DesktopInboxBridge (in desktop-layout.tsx), so it has the
// navigation + workspace-slug context the bridge needs.

import { useEffect, useRef } from "react";
import { useSyncExternalStore } from "react";
import { getCurrentSlug, subscribeToCurrentSlug } from "@multica/core/platform";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useNavigation } from "@multica/views/navigation";

export function useCerebroBrowserBridge(): void {
  const enabled = useFeatureFlag("cerebro_browser");
  const { push } = useNavigation();

  // Reactive current workspace slug from the platform singleton. Mirrors how
  // DesktopShell reads it; null until WorkspaceRouteLayout sets it on mount.
  const slug = useSyncExternalStore(
    subscribeToCurrentSlug,
    getCurrentSlug,
    () => null,
  );

  // Keep push + slug in refs so the open-tab subscription stays stable across
  // navigations (the adapter identity changes with the active tab's location).
  const pushRef = useRef(push);
  const slugRef = useRef(slug);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);
  useEffect(() => {
    slugRef.current = slug;
  }, [slug]);

  // (1) Bring up the control server once, when the flag is on and the bridge
  // (window.cerebroBrowser) exists (desktop only).
  const ensuredRef = useRef(false);
  useEffect(() => {
    if (!enabled || ensuredRef.current) return;
    const api = typeof window !== "undefined" ? window.cerebroBrowser : undefined;
    if (!api) return;
    ensuredRef.current = true;
    void api.ensureControlServer().catch(() => {
      // Best-effort: a flag-on workspace that is not signed in or where the
      // feature did not come up just leaves the agent transport down. The CLI
      // surfaces a clear error in that case.
      ensuredRef.current = false;
    });
  }, [enabled]);

  // (2) Register the open-tab listener while the flag is on.
  useEffect(() => {
    if (!enabled) return;
    const api = typeof window !== "undefined" ? window.cerebroBrowser : undefined;
    if (!api) return;
    return api.onOpenTab(() => {
      const s = slugRef.current;
      if (!s) return;
      pushRef.current(`/${s}/cerebro/browser`);
    });
  }, [enabled]);
}
