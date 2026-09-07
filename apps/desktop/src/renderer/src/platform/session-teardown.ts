/**
 * What the end of a session tears down on Desktop — and what it deliberately
 * leaves alone.
 *
 * Two different events arrive here, and they differ in exactly one place. An
 * explicit logout is the user handing the machine back, so everything goes,
 * including the local daemon. A session the server ended (401) clears the same
 * window state — the login screen it lands on will take anyone's credentials —
 * but leaves the daemon alone: it holds its own PAT, minted separately from
 * this window's session token, and may be running agent work right now. An
 * expiring UI credential is no reason to kill it (MUL-7028).
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
 * Session expiry: the same window teardown as a logout, because the screen it
 * lands on offers a login form and the next person to use it may not be the
 * one who was just signed in. Tabs go with it — their paths carry workspace
 * slugs and issue ids, and leaving them for the workspace validator to prune
 * catches nothing at all when the next user shares a workspace with the last.
 *
 * The daemon is the whole difference between the two paths, and the reason
 * this file exists: it authenticates with its own PAT and may be running agent
 * work, so a UI credential expiring must not take it down.
 */
export function tearDownOnSessionExpiry(t: SessionTeardown): void {
  t.reportAuthSession(null);
  t.resetTabs();
  t.closeOverlay();
  t.resetWelcome();
}
