import assert from "node:assert/strict";
import test from "node:test";

import { invalidForTiming } from "./nav-trace-timing.mjs";

// MUL-7095: pin sampler-error handling without breaking the
// collect/accept split (Case 1~3 from the follow-up spec).
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

test("Case 3: collect + non-zero exit + empty errors keeps known-bad timing", () => {
  const trace = validTrace();
  trace.acceptance = "collect";
  assert.equal(invalidForTiming({ scenario_exit: 1, trace }), false);
});
