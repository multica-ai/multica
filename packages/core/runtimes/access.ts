import type { RuntimeDevice } from "../types";

/**
 * Whether `runtime` may EXECUTE an agent owned by `agentOwnerId`.
 *
 * Distinct from isRuntimeUsableForUser, which answers "may the person at the
 * keyboard bind an agent here". Both gates apply, and they differ exactly when
 * the operator is not the agent's owner — an admin reassigning a teammate's
 * agent, say. A private machine spends ITS OWNER's credentials and local files,
 * so the question of whose agent may run there is a property of the agent's
 * owner, not of who is clicking (MUL-6704).
 *
 * The server enforces the same predicate at three layers
 * (service.RuntimeAllowsAgentOwner, the SQL claim fence, and the post-claim
 * recheck), so a picker that ignored it would offer a target the API refuses —
 * or worse, bind an agent whose every task is then refused at claim time.
 *
 * `undefined` means "not known yet" — a create surface where the owner will be
 * the current user, or a list still loading — and is permissive, like
 * isRuntimeUsableForUser's unknown viewer. `null` is different: it means the
 * agent genuinely HAS no owner, which the server refuses on a private runtime
 * (it cannot mint a task token), so a picker must refuse it too rather than offer
 * a choice that 403s on submit.
 */
export function isRuntimeUsableForAgentOwner(
  runtime: RuntimeDevice,
  agentOwnerId: string | null | undefined,
): boolean {
  if (!runtime.owner_id) return false;
  if (runtime.visibility === "public") return true;
  if (agentOwnerId === undefined) return true;
  return runtime.owner_id === agentOwnerId;
}

/**
 * Whether this member may run an agent on `runtime`.
 *
 * A private runtime is usable only by its owner; a public one by anyone in the
 * workspace; an ownerless one by nobody. Workspace role does not enter into
 * it: a private runtime is someone's own machine, so not even an admin may
 * bind an agent to it, and only the owner may flip it to public. The server
 * enforces exactly this predicate (`canUseRuntimeForAgent`), so the picker and
 * the API/CLI never disagree (MUL-6126).
 *
 * An unknown viewer (no session yet) is treated as allowed so a still-loading
 * auth state never hides a runtime the user does own — every write path
 * re-checks server-side.
 *
 * This is the single source of truth for "can this runtime be picked": every
 * create / duplicate / builder / reassign surface must call it rather than
 * re-deriving the rule.
 */
export function isRuntimeUsableForUser(
  runtime: RuntimeDevice,
  currentUserId: string | null,
): boolean {
  // An ownerless runtime is unusable by anyone, public included: the server
  // needs an owner to mint the agent's task token, so a bind here would only
  // yield agents whose tasks get cancelled at claim time (MUL-3292).
  if (!runtime.owner_id) return false;
  if (!currentUserId) return true;
  if (runtime.owner_id === currentUserId) return true;
  return runtime.visibility === "public";
}
