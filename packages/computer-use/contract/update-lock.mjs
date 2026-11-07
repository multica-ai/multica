// Appends the working tree's Computer contract state to the lock after a
// version bump. Run: node packages/computer-use/contract/update-lock.mjs
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { bumpProblems, currentEntry, lockPath } from "./lock.mjs";

const history = existsSync(lockPath) ? JSON.parse(readFileSync(lockPath, "utf8")).history : [];
const current = currentEntry();
const last = history.at(-1);
if (last && JSON.stringify(last) === JSON.stringify(current)) {
  console.log(`computer contract lock already at ${current.version}`);
} else {
  const problems = last ? bumpProblems(last, current) : [];
  if (problems.length) {
    console.error(problems.join("\n"));
    process.exit(1);
  }
  history.push(current);
  writeFileSync(lockPath, JSON.stringify({ history }, null, 2) + "\n");
  console.log(`computer contract lock → ${current.version}`);
}
