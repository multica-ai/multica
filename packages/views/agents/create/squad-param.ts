/**
 * Builds a creation-route URL with the params that have to survive a hop
 * between screens.
 *
 * `squad` is the reason the flow was opened at all, so it rides along from the
 * chooser into whichever method the user picks.
 *
 * `runtime` is a seed, not an identity: a conversation joins the sessions list
 * only once it holds a message or a saved configuration, so for the first turn
 * the runtime the user just picked cannot be read back from anywhere. It rides
 * in the URL rather than in component state so a refresh on that first turn
 * still knows where the conversation runs.
 */
export function createPathWithParams(
  path: string,
  params: { squad?: string | null; runtime?: string | null },
): string {
  const query = new URLSearchParams();
  if (params.squad) query.set("squad", params.squad);
  if (params.runtime) query.set("runtime", params.runtime);
  const suffix = query.toString();
  return suffix ? `${path}?${suffix}` : path;
}

export function withSquadParam(path: string, squadId: string | null): string {
  return createPathWithParams(path, { squad: squadId });
}
