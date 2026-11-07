import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { bumpProblems, lockPath, lockProblems } from "./lock.mjs";

const entry = (version, contract = "c1", prompts = "p1") => ({ version, contractSha256: contract, promptsSha256: prompts });

test("the lock matches the working tree", () => {
  assert.deepEqual(lockProblems(JSON.parse(readFileSync(lockPath, "utf8")).history), []);
});

test("contract changes need a minor or major bump", () => {
  assert.equal(bumpProblems(entry("1.0.0"), entry("1.0.1", "c2")).length, 1);
  assert.deepEqual(bumpProblems(entry("1.0.0"), entry("1.1.0", "c2")), []);
  assert.deepEqual(bumpProblems(entry("1.0.0"), entry("2.0.0", "c2")), []);
});

test("prompt changes need at least a patch bump", () => {
  assert.equal(bumpProblems(entry("1.0.0"), entry("1.0.0", "c1", "p2")).length, 1);
  assert.deepEqual(bumpProblems(entry("1.0.0"), entry("1.0.1", "c1", "p2")), []);
});

test("an unbumped change in the working tree is reported", () => {
  assert.equal(lockProblems([entry("1.0.0")], entry("1.0.0", "c2")).length, 1);
  assert.match(lockProblems([entry("1.0.0")], entry("1.1.0", "c2"))[0], /update-lock/);
});
