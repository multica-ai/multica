/**
 * MUL-7095: timing-sample validity shared by the runner and its unit pin.
 *
 * A trace is timing-usable only when its full content matches the common
 * spec assertions (ordering invariant, populated+initialized sample,
 * primary metric) AND the sampler loop itself stayed healthy.
 *
 * `collect` mode (base side) forgives ONLY the known-bad base's
 * blank-frame acceptance miss — a sampler-loop failure invalidates the
 * sample in BOTH modes, since a throw may have skipped exactly the frames
 * where a blank state would have been recorded.
 *
 * @param entry runner repeat entry `{ scenario_exit, trace }`
 * @returns `true` when the sample must NOT count for the guardrail.
 */
export const invalidForTiming = (entry) => {
  const trace = entry?.trace;
  if (!trace || trace.status !== "ok") return true;
  // Measurement-infrastructure invariant (BOTH modes): any sampler error
  // invalidates the timing sample.
  if (!Array.isArray(trace.samplerErrors) || trace.samplerErrors.length > 0)
    return true;
  // Common spec assertions (both modes): the full ordering invariant,
  // a populated+initialized sample, and the primary metric.
  if (typeof trace.clickT !== "number") return true;
  if (typeof trace.firstDetailCommitT !== "number") return true;
  if (typeof trace.firstHostT !== "number") return true;
  if (typeof trace.firstPopulatedT !== "number") return true;
  if (typeof trace.clickToCommitMs !== "number" || trace.clickToCommitMs <= 0)
    return true;
  if (trace.firstHostT < trace.firstDetailCommitT) return true;
  if (trace.firstPopulatedT < trace.firstHostT) return true;
  if (typeof trace.clickToPopulatedMs !== "number") return true;
  if (trace.clickToPopulatedMs < trace.clickToCommitMs) return true;
  if (
    !Array.isArray(trace.samples) ||
    !trace.samples.some((s) => s.populated && s.editorInitialized === true)
  )
    return true;
  if (entry.scenario_exit === 0) return false;
  // Non-zero exit in `collect` mode is an acceptance miss on a side whose
  // correctness is not under test — the timing sample still stands.
  if (trace.acceptance === "collect") return false;
  return true;
};
