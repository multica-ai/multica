import { useEffect, useRef } from "react";
import { paths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";

/**
 * Opens the issue named by a `multica://<slug>/issues/<id>` deep link.
 *
 * The main process owns the OS protocol registration and forwards only the
 * parsed identifiers, so the route is built here — the renderer is the side
 * that owns `paths`. Like the notification bridge in desktop-layout, this
 * routes through `useNavigation().push` rather than the `multica:navigate`
 * event: push lets the navigation adapter turn a cross-workspace path into
 * `switchWorkspace(slug, path)`, so a link for workspace A opens A instead of
 * mounting A's issue inside B's tab group.
 *
 * Delivery is queued in main until this listener exists, which covers the two
 * cold cases: the app was not running, and the app was running but signed out
 * (the link is delivered once the shell mounts after login).
 */
export function IssueDeepLinkBridge() {
  const { push } = useNavigation();
  // The adapter identity changes with the active tab's location; the ref keeps
  // the main-process subscription stable across navigations.
  const pushRef = useRef(push);
  useEffect(() => {
    pushRef.current = push;
  }, [push]);

  useEffect(() => {
    return window.desktopAPI.onIssueOpen(({ slug, issueId }) => {
      if (!slug || !issueId) return;
      pushRef.current(paths.workspace(slug).issueDetail(issueId));
    });
  }, []);

  return null;
}
