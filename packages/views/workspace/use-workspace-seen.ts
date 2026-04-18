import { useRef } from "react";

/**
 * Tracks workspace slugs that have successfully resolved to a workspace at
 * least once during this layout instance's lifetime. Used to distinguish:
 *
 *  - "Active workspace was just removed" (slug seen before, now gone) —
 *    the caller is typically navigating away (delete/leave mutation, or
 *    realtime workspace:deleted event). Rendering NoAccessPage during
 *    that window causes a jarring flash of "Workspace not available"
 *    before the navigate completes. Return `true` so the layout can
 *    render null while the navigate resolves.
 *
 *  - "URL points to a workspace I've never had access to" (slug never
 *    seen) — genuine access-denial case. Return `false` so the layout
 *    can render NoAccessPage with its recovery buttons.
 *
 * Scope: one Set per layout instance. If the workspace layout unmounts
 * (e.g. desktop tab close), the memory is discarded — correct, since the
 * user lost that view anyway.
 */
export function useWorkspaceSeen(
  slug: string | undefined,
  resolved: boolean,
): boolean {
  const seenRef = useRef<Set<string>>(new Set());
  if (resolved && slug) seenRef.current.add(slug);
  return slug ? seenRef.current.has(slug) : false;
}
