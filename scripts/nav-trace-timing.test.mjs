import assert from "node:assert/strict";
import test from "node:test";

import { invalidForTiming, relativeRegression } from "./nav-trace-timing.mjs";

// MUL-7095: pin sampler-error and exit-code handling across the
// collect/accept split (Case 1~5 from the follow-up spec).
const validTrace = () => ({
  status: "ok",
  acceptance: "accept",
  clickT: 100,
  firstDetailCommitT: 200,
  firstHostT: 200,
  firstPopulatedT: 200,
  clickToCommitMs: 100,
  clickToPopulatedMs: 100,
  samplerErrors: [],
  samples: [{ populated: true, editorInitialized: true }],
});

test("Case 1: empty samplerErrors keeps the existing valid semantics", () => {
  assert.equal(invalidForTiming({ scenario_exit: 0, trace: validTrace() }), false);
});

test("Case 2: one sampler error invalidates even with perfect timing", () => {
  const trace = validTrace();
  trace.samplerErrors = [{ t: 100, message: "boom" }];
  assert.equal(invalidForTiming({ scenario_exit: 0, trace }), true);
});

test("Case 3: collect mode does not forgive a non-zero exit", () => {
  const trace = validTrace();
  trace.acceptance = "collect";
  assert.equal(invalidForTiming({ scenario_exit: 1, trace }), true);
});

// The known-bad base is still measured: it exits 0 in collect mode because
// the spec skips the blank-frame and retained-surface assertions there
// (measured 5/5, nav-fix-r6). Only that, never a failed process.
test("Case 4: the known-bad base stays usable when its collect run exits 0", () => {
  const trace = validTrace();
  trace.acceptance = "collect";
  trace.blankFrames = 2;
  trace.retainedSurface = false;
  assert.equal(invalidForTiming({ scenario_exit: 0, trace }), false);
});

test("Case 5: accept mode with a non-zero exit stays unusable", () => {
  assert.equal(
    invalidForTiming({ scenario_exit: 1, trace: validTrace() }),
    true,
  );
});

// MUL-7095: zero-baseline regression policy for the A/B guardrail.
// The comparison imports this helper from the timing module so the unit pin
// never executes the runner entrypoint.
const ALLOWANCE = 0.25;

test("zero baseline 0 -> 0 is an available passing comparison", () => {
  const result = relativeRegression(0, 0, ALLOWANCE);
  assert.equal(result.available, true);
  assert.equal(result.ratio, null);
  assert.equal(result.failed, false);
});

test("zero baseline 0 -> >0 is an available failing comparison", () => {
  const result = relativeRegression(0, 12.5, ALLOWANCE);
  assert.equal(result.available, true);
  assert.equal(result.ratio, null);
  assert.equal(result.failed, true);
});

test("positive baseline within 25% passes", () => {
  const result = relativeRegression(100, 110, ALLOWANCE);
  assert.equal(result.available, true);
  assert.equal(result.failed, false);
});

test("positive baseline above 25% fails", () => {
  const result = relativeRegression(100, 130, ALLOWANCE);
  assert.equal(result.available, true);
  assert.equal(result.failed, true);
});

test("exact 1.25x boundary passes (fail only when strictly greater)", () => {
  const result = relativeRegression(100, 125, ALLOWANCE);
  assert.equal(result.available, true);
  assert.equal(result.failed, false);
});

test("invalid or missing data stays unavailable and fail-closed", () => {
  for (const [base, head] of [
    [null, 0],
    [0, null],
    [undefined, undefined],
    [Number.NaN, 0],
    [0, Number.NaN],
    [Number.POSITIVE_INFINITY, 0],
    [100, Number.POSITIVE_INFINITY],
  ]) {
    const result = relativeRegression(base, head, ALLOWANCE);
    assert.equal(result.available, false);
  }
  // The gate treats every unavailable comparison as a failure.
  const gateFails = (regression) => !regression.available || regression.failed;
  assert.equal(gateFails(relativeRegression(null, 0, ALLOWANCE)), true);
  assert.equal(gateFails(relativeRegression(0, 0, ALLOWANCE)), false);
  assert.equal(gateFails(relativeRegression(0, 5, ALLOWANCE)), true);
});
