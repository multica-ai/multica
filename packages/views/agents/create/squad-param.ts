/**
 * Builds a creation-route URL with the params that have to survive a hop
 * between screens.
 *
 * `squad` is the reason the flow was opened at all, so it rides along from the
 * chooser into whichever method the user picks. `session` addresses one builder
 * conversation, so a refresh, a back/forward, or a reopened tab lands back in
 * the same chat instead of starting a new one.
 */
export function createPathWithParams(
  path: string,
  params: { squad?: string | null; session?: string | null },
): string {
  const query = new URLSearchParams();
  if (params.squad) query.set("squad", params.squad);
  if (params.session) query.set("session", params.session);
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

export function withSquadParam(path: string, squadId: string | null): string {
  return createPathWithParams(path, { squad: squadId });
}
