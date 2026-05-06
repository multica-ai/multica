/**
 * Result returned by the local-reset orchestrator. Shared between the main
 * process implementation and the renderer/preload type declaration so both
 * sides agree on the shape without the renderer importing from `main/`.
 */
export type ResetResult = {
  /** True iff stopStack succeeded and every reachable target was removed. */
  ok: boolean;
  /** Absolute paths the orchestrator successfully removed. */
  removed: string[];
  /** Targets the orchestrator deliberately skipped (with a reason in the log). */
  skipped: string[];
  /** Per-target failures. Entries with `path === "stopStack"` represent a
   *  pre-deletion supervisor stop that threw — recorded but never fatal. */
  errors: { path: string; error: string }[];
};
