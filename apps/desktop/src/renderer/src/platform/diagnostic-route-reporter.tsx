import { useEffect, useRef } from "react";
import { bucketDiagnosticPath, setDiagnosticRoute } from "@multica/core/diagnostics";
import { useAuthStore } from "@multica/core/auth";
import { useActiveTabIdentity, useActiveTabUrl } from "@/stores/tab-store";
import {
  useWindowOverlayStore,
  type WindowOverlay,
} from "@/stores/window-overlay-store";
import type { RendererRouteContextInput } from "../../../shared/renderer-route-context";

/**
 * Publishes which page this window is showing, for freeze diagnostics.
 *
 * Two consumers, because a freeze can be observed from two places:
 *
 *   - The main process, via IPC. It is the only party still alive during a
 *     true hang, and it cannot ask a blocked renderer anything — so the route
 *     has to already be there when the hang starts.
 *   - The in-renderer freeze watchdog, via the shared diagnostic context.
 *     `location.pathname` is the packaged index.html path here (this window
 *     runs a memory router), so without this the watchdog has no route at all.
 *
 * Desktop has three layers that can own the visible page: the logged-out
 * `/login` state, window overlays (onboarding, new-workspace, invite — these
 * are overlay state, not tab routes), and otherwise the active tab's path.
 *
 * Paths are bucketed to route templates before publishing, so telemetry
 * carries `/:slug/issues/:id` rather than a workspace slug and issue id.
 */
export function DiagnosticRouteReporter() {
  const user = useAuthStore((s) => s.user);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  // The slug decides whether a workspace is mounted at all; it is never sent.
  const { slug: activeWorkspaceSlug } = useActiveTabIdentity();
  // The tab url carries search/hash too; bucketing drops both, so telemetry
  // never sees a filter value or an anchor.
  const activeTabUrl = useActiveTabUrl();

  // Last payload pushed to main, so a re-render doesn't re-send an identical
  // context.
  const lastSentRef = useRef<string | null>(null);

  useEffect(() => {
    const surface = resolveSurface({
      user,
      overlay,
      activeWorkspaceSlug,
      activeTabUrl,
    });
    setDiagnosticRoute(surface.path);

    // Only the bucketed template travels. The slug and tab id stay in this
    // process: they identify a workspace and a session, and this payload ends
    // up in telemetry.
    const context: RendererRouteContextInput = {
      surface: surface.kind,
      path: surface.path,
    };
    send(context, lastSentRef);
  }, [user, overlay, activeWorkspaceSlug, activeTabUrl]);

  return null;
}

function send(
  context: RendererRouteContextInput,
  lastSentRef: { current: string | null },
) {
  const serialized = JSON.stringify(context);
  if (serialized === lastSentRef.current) return;
  lastSentRef.current = serialized;
  window.desktopAPI.setRendererRouteContext(context);
}

function resolveSurface({
  user,
  overlay,
  activeWorkspaceSlug,
  activeTabUrl,
}: {
  user: unknown;
  overlay: WindowOverlay | null;
  activeWorkspaceSlug: string | null;
  activeTabUrl: string | null;
}): { kind: RendererRouteContextInput["surface"]; path: string } {
  if (!user) return { kind: "login", path: "/login" };
  if (overlay) {
    return { kind: "overlay", path: bucketDiagnosticPath(overlayPath(overlay)) };
  }
  if (activeWorkspaceSlug && activeTabUrl) {
    return { kind: "tab", path: bucketDiagnosticPath(activeTabUrl) };
  }
  return { kind: "tab", path: "/" };
}

function overlayPath(overlay: WindowOverlay): string {
  switch (overlay.type) {
    case "new-workspace":
      return "/workspaces/new";
    case "onboarding":
      return "/onboarding";
    case "invite":
      return `/invite/${overlay.invitationId}`;
    case "invitations":
      return "/invitations";
  }
}
