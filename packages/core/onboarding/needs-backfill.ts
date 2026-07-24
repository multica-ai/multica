import type { User } from "../types";
import type { QuestionnaireAnswers } from "./types";

/**
 * Maximum number of times the user can close the backfill prompt with
 * the X / ESC / outside-click before we treat it as a permanent
 * dismissal. After that the prompt stops appearing.
 *
 * Submit and explicit Skip are always terminal (they write to the
 * server). The count exists only for the "I'll think about it later"
 * close path — without a cap, a user who never decides would see the
 * prompt every login forever.
 */
export const SOURCE_BACKFILL_MAX_DISMISSALS = 3;

/**
 * Minimum number of issues completed by an AI assignee (agent or
 * squad) in the current workspace before the source prompt may open.
 *
 * Source is not asked during onboarding at all — attribution is a
 * zero-payoff question for the user, so we wait until Multica has
 * demonstrably delivered value (agents finished real work) before
 * spending goodwill on it. Answer rates for "how did you hear about
 * us" prompts are also materially better after an activation moment
 * than at signup. 3 ≈ one Helper starter-task batch, so an engaged
 * new user typically crosses it within the first session.
 *
 * The count itself comes from `agentCompletedIssueCountOptions` in
 * `./queries.ts`; the modal combines it with `needsSourceBackfill`.
 */
export const SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES = 3;

/**
 * Should we ask this already-onboarded user where they heard about
 * Multica?
 *
 * Returns true for users who:
 *  - have completed onboarding (`onboarded_at` set), and
 *  - have not recorded any source (empty array or absent), and
 *  - did not previously decline the source question (skip marker), and
 *  - have not closed this backfill prompt enough times to dismiss it.
 *
 * This is the user-level half of the gate. The workspace-level half —
 * "have agents completed at least SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES
 * issues here?" — needs a server query, so it lives in the modal
 * (`source-backfill-modal.tsx`), which also uses this predicate to
 * decide whether that query is worth running at all.
 *
 * Pure function — `dismissCount` is passed in so this stays callable
 * from core (no localStorage / StorageAdapter dependency).
 */
export function needsSourceBackfill(
  user: User | null | undefined,
  dismissCount: number,
): boolean {
  if (!user) return false;
  if (!user.onboarded_at) return false;
  if (dismissCount >= SOURCE_BACKFILL_MAX_DISMISSALS) return false;

  const q = user.onboarding_questionnaire as
    | Partial<QuestionnaireAnswers>
    | null
    | undefined;
  if (!q) return true;
  if (q.source_skipped === true) return false;
  // Pre-multi-select rows wrote `source` as a bare string. Treat a
  // non-empty string the same as a one-element array — the user did
  // answer. Mirrors `OnboardingFlow.mergeQuestionnaire` (views) and
  // `stringOrSlice.UnmarshalJSON` (server).
  const raw: unknown = q.source;
  if (Array.isArray(raw)) return raw.length === 0;
  if (typeof raw === "string") return raw.length === 0;
  return true;
}
