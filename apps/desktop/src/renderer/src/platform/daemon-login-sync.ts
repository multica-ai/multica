/**
 * Bring the local daemon up for a freshly signed-in user.
 *
 * The order matters and is the reason this is a function rather than three
 * inline awaits. Desktop's daemon profile is derived from the target API URL,
 * so until the main process has that URL there is no Desktop-owned profile —
 * and main now refuses to write the token at all rather than fall back to the
 * user's default CLI profile at `~/.multica/` (#6399).
 *
 * That refusal has no retry of its own: the login effect re-runs on `user`, not
 * on the target URL. So the URL must be pushed and awaited here, before the
 * token sync, instead of racing a separate effect's IPC message. Re-sending it
 * is cheap — the main-process handler ignores an unchanged value.
 */
export interface DaemonLoginSyncAPI {
  setTargetApiUrl: (url: string) => Promise<void>;
  syncToken: (token: string, userId: string) => Promise<void>;
  autoStart: () => Promise<unknown>;
}

export async function syncDaemonOnLogin(
  api: DaemonLoginSyncAPI,
  apiUrl: string,
  token: string,
  userId: string,
): Promise<void> {
  await api.setTargetApiUrl(apiUrl);
  await api.syncToken(token, userId);
  await api.autoStart();
}
