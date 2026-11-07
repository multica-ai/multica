// Version-bump rules for the Computer capability. The lock is the history of
// released contract states: every change must come with a version bump, a
// contract change with a new major or minor.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const lockPath = resolve(here, "computer.lock.json");
const sha256 = (path) => createHash("sha256").update(readFileSync(path)).digest("hex");
const majorMinor = (version) => version.split(".").slice(0, 2).join(".");

/** The contract state of the working tree. */
export function currentEntry() {
  const { version, ...contract } = JSON.parse(readFileSync(resolve(here, "computer.json"), "utf8"));
  // The version is excluded so a prompt-only patch bump keeps the contract hash.
  const contractSha256 = createHash("sha256").update(JSON.stringify(contract)).digest("hex");
  return { version, contractSha256, promptsSha256: sha256(resolve(here, "../prompts.mjs")) };
}

/** Problems with appending next after previous; empty when the bump is valid. */
export function bumpProblems(previous, next) {
  const problems = [];
  if (next.contractSha256 !== previous.contractSha256 && majorMinor(next.version) === majorMinor(previous.version))
    problems.push(`contract changed: bump the minor or major version of ${previous.version}`);
  if (next.promptsSha256 !== previous.promptsSha256 && next.version === previous.version)
    problems.push(`prompts changed: bump at least the patch version of ${previous.version}`);
  return problems;
}

/** Problems with the lock against the working tree; empty when consistent. */
export function lockProblems(history, current = currentEntry()) {
  const problems = [];
  for (let i = 1; i < history.length; i++) problems.push(...bumpProblems(history[i - 1], history[i]).map((p) => `history ${history[i].version}: ${p}`));
  const last = history.at(-1);
  if (!last) return ["lock history is empty"];
  if (JSON.stringify(last) !== JSON.stringify(current)) {
    const bump = bumpProblems(last, current);
    problems.push(bump.length ? bump.join("; ") : "contract version changed: run node packages/computer-use/contract/update-lock.mjs");
  }
  return problems;
}
