/**
 * What the end of a session tears down on Desktop — and what it deliberately
 * leaves alone.
 *
 * Two different events arrive here and they are not the same event. An
 * explicit logout is the user handing the machine back, so everything goes,
 * including the local daemon. A session the server ended (401) is not that:
 * the daemon holds its own PAT, minted separately from this window's session
 * token, and may be running agent work right now. An expiring UI credential is
 * no reason to kill it (MUL-7028) — only this window has to sign in again.
 *
 * An account switch stays safe without the expiry path stopping anything,
 * because it is handled at the other end: `daemon:sync-token` mints a fresh
 * PAT and restarts the daemon whenever the user id changes.
 *
 * Side effects arrive as injected callbacks — the same shape as
 * `daemon-login-sync` — so the difference between the two paths is testable
 * without an Electron window.
 */
export interface SessionTeardown {
  /** Report the account transition to the main process. */
  reportAuthSession: (userId: string | null) => void;
  /** Desktop tab layout, which can name workspaces and issues. */
  resetTabs: () => void;
  /** Any pre-workspace overlay left open (invite, onboarding, …). */
  closeOverlay: () => void;
  /** The one-shot post-onboarding welcome signal. */
  resetWelcome: () => void;
  /** Credential the local daemon authenticates with. */
  clearDaemonToken: () => Promise<unknown>;
  /** The local daemon process itself. Resolves to an IPC result we ignore —
   *  a daemon that was already down is success enough. */
  stopDaemon: () => Promise<unknown>;
}

/**
 * Explicit logout: wipe desktop-only in-memory state and stop the daemon, so a
 * subsequent login as a different user inherits none of the previous user's
 * tabs, overlay, or credentials. Zustand persist only writes to localStorage;
 * `useLogout` clears the storage key, but the live stores stay populated until
 * they are reset here.
 */
export async function tearDownOnLogout(t: SessionTeardown): Promise<void> {
  // Report synchronously before the async daemon cleanup, so a rapidly closed
  // main window cannot leave authenticated issue renderers behind.
  t.reportAuthSession(null);
  t.resetTabs();
  t.closeOverlay();
  t.resetWelcome();
  try {
    await t.clearDaemonToken();
  } catch {
    // Best-effort — clearing is followed by stop, which also hardens state.
  }
  try {
    await t.stopDaemon();
  } catch {
    // Daemon may already be stopped.
  }
}

/**
 * Session expiry: drop only the window state that would be wrong for whoever
 * signs in next. The daemon keeps running, and so do the tabs — the same
 * person is usually about to sign straight back in, and if someone else signs
 * in instead, `AppContent` re-validates every tab group against the new user's
 * workspace list.
 */
export function tearDownOnSessionExpiry(t: SessionTeardown): void {
  t.reportAuthSession(null);
  t.closeOverlay();
  t.resetWelcome();
}
