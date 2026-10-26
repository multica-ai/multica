import type { useT } from "../i18n";

// Queue-wait constraint labels for a queued run's inline summary (#8551):
// the phrase alone is the summary line, naming the constraint the server's
// last claim poll recorded on the run's queue row. The queued status itself
// is announced by the run's status chip, so the phrase must not repeat it.
//
// Phrases state only what the recorded value proves. attempted_unclaimed is
// deliberately neutral ("queue busy" class): its common cause is another
// claimer winning the row — normal scheduler competition, not a fault — and
// the server cannot tell that apart from a claim fence. An unrecognized value
// (newer server, or a constraint this build predates) returns undefined so
// the caller keeps the plain queued copy instead of exposing a raw enum
// token to the user.
type IssuesT = ReturnType<typeof useT<"issues">>["t"];

export function queueConstraintPhrase(constraint: string | undefined, t: IssuesT): string | undefined {
  switch (constraint) {
    case "agent_capacity":
      return t(($) => $.inline_run.queue_constraint_agent_capacity);
    case "attempted_unclaimed":
      return t(($) => $.inline_run.queue_constraint_attempted_unclaimed);
    case "agent_rebound":
      return t(($) => $.inline_run.queue_constraint_agent_rebound);
    case "agent_unbound":
      return t(($) => $.inline_run.queue_constraint_agent_unbound);
    default:
      return undefined;
  }
}
