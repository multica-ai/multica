#!/usr/bin/env node
// Path filtering and aggregate gates share this fail-closed decision contract.
import { appendFileSync, readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export const filters = JSON.parse(
  readFileSync(new URL("../.github/ci-paths.json", import.meta.url), "utf8"),
);

export function decideScopes(event, filtered) {
  const full = event === "schedule" || event === "workflow_dispatch";
  if (!full && event !== "push" && event !== "pull_request") {
    throw new Error(`Unsupported CI event: ${event}`);
  }
  const outputs = { full: String(full) };
  for (const scope of Object.keys(filters)) {
    if (!full && filtered[scope] !== "true" && filtered[scope] !== "false") {
      throw new Error(`Missing or invalid path-filter result: ${scope}`);
    }
    outputs[scope] = full ? "true" : filtered[scope];
  }
  // Product builds already install dependencies and run the shared checks.
  // Only allocate a separate runner when quality is the sole frontend work.
  outputs.quality_only = String(outputs.quality === "true" && outputs.frontend === "false");
  return outputs;
}

export function checkGate(needs, jobScopes) {
  if (needs.changes?.result !== "success") {
    throw new Error("Path filtering did not succeed");
  }
  for (const [job, scope] of Object.entries(jobScopes)) {
    const selected = needs.changes.outputs?.[scope];
    if (selected !== "true" && selected !== "false") {
      throw new Error(`Missing or invalid scope: ${scope}`);
    }
    const expected = selected === "true" ? "success" : "skipped";
    if (needs[job]?.result !== expected) {
      throw new Error(`${job}: expected ${expected}, got ${needs[job]?.result ?? "missing"}`);
    }
  }
  // A newly added dependency must have a declared scope before the gate can pass.
  for (const job of Object.keys(needs)) {
    if (job !== "changes" && !Object.hasOwn(jobScopes, job)) {
      throw new Error(`Unchecked dependency: ${job}`);
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    switch (process.argv[2]) {
      case "decide": {
        const outputs = decideScopes(process.env.EVENT_NAME, JSON.parse(process.env.FILTER_RESULTS));
        appendFileSync(process.env.GITHUB_OUTPUT,
          Object.entries(outputs).map(([key, value]) => `${key}=${value}\n`).join(""));
        break;
      }
      case "gate":
        checkGate(JSON.parse(process.env.NEEDS_JSON), JSON.parse(process.env.JOB_SCOPES));
        console.log("All selected CI jobs succeeded; only unselected jobs were skipped.");
        break;
      default:
        throw new Error("Usage: node scripts/ci-scope.mjs decide|gate");
    }
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
