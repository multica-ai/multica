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
 * Should we ask this already-onboarded user where they heard about
 * Multica?
 *
 * Returns true for users who:
 *  - have completed onboarding (`onboarded_at` set), and
 *  - have not recorded any source (empty array or absent), and
 *  - did not previously click Skip on the onboarding `source` step, and
 *  - have not closed this backfill prompt enough times to dismiss it.
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
  const source = Array.isArray(q.source) ? q.source : [];
  return source.length === 0;
}
