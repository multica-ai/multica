/**
 * Route gate for the floating chat overlay, shared by the overlay itself and by
 * the `toggleChat` keyboard shortcut so the two can never disagree.
 *
 * On the Chat tab the full-page surface already owns the same `activeSessionId`,
 * so a floating copy would be pure duplication — `FloatingChat` renders nothing
 * there. The shortcut has to honour the same rule: flipping `isOpen` on a route
 * where the overlay cannot mount reads as a dead keypress, and then surprises
 * the user with a window that pops open (or vanishes) on the next navigation.
 * The Team page drives the same store for its agent chat, so it is suppressed
 * too.
 */
export function isFloatingChatRouteSuppressed(
  pathname: string,
  ...surfacePaths: string[]
): boolean {
  return surfacePaths.some(
    (path) => pathname === path || pathname.startsWith(`${path}/`),
  );
}
