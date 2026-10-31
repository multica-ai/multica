/**
 * Desktop main windows each show one workspace (the VS Code folder model).
 * These helpers send a workspace this window does not show to the window
 * that does, and open other workspaces in windows of their own. Every call
 * tolerates an older preload that lacks the multi-window bridge.
 */

function isMainWindow(): boolean {
  return (window.desktopAPI.windowContext?.kind ?? "main") === "main";
}

/**
 * Hand `slug` — and `path` inside it, when given — to another window that
 * already shows that workspace. Returns false when no other window does, so
 * the caller switches its own workspace instead.
 */
export function handOffToWorkspaceWindow(slug: string, path?: string): boolean {
  if (!isMainWindow()) return false;
  return window.desktopAPI.focusWorkspaceWindow?.(slug, path) ?? false;
}

export function openWorkspaceInNewWindow(slug: string): void {
  void window.desktopAPI.openWorkspaceWindow?.(slug);
}

export function otherWindowWorkspaces(): Set<string> {
  return new Set(window.desktopAPI.getOtherWindowWorkspaces?.() ?? []);
}

/** The workspace main asked this window to open, if it was opened for one. */
export function launchWorkspaceSlug(): string | null {
  const context = window.desktopAPI.windowContext;
  return context?.kind === "main" ? (context.workspaceSlug ?? null) : null;
}

/** The first workspace that no window shows yet, for Cmd/Ctrl+Shift+N. */
export function nextWorkspaceForNewWindow(
  slugs: readonly string[],
  activeSlug: string | null,
  openElsewhere: ReadonlySet<string>,
): string | null {
  return (
    slugs.find((slug) => slug !== activeSlug && !openElsewhere.has(slug)) ?? null
  );
}

/**
 * The workspace a window should show once the list is known. A window keeps
 * its workspace unless another window already shows it and this window was
 * not opened for it — e.g. a pre-multi-window session restoring the shared
 * last-active workspace next to a window restored for that same workspace.
 */
export function resolveWindowWorkspace(
  slugs: readonly string[],
  activeSlug: string | null,
  launchSlug: string | null,
  openElsewhere: ReadonlySet<string>,
): string | null {
  if (
    activeSlug &&
    (activeSlug === launchSlug || !openElsewhere.has(activeSlug))
  ) {
    return activeSlug;
  }
  const free = slugs.find((slug) => !openElsewhere.has(slug));
  return free ?? activeSlug ?? slugs[0] ?? null;
}
