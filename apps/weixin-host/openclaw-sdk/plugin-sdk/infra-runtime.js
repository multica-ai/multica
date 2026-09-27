import os from "node:os";
import path from "node:path";

export function resolvePreferredOpenClawTmpDir() {
  const stateDir = process.env.OPENCLAW_STATE_DIR?.trim();
  return stateDir ? path.join(stateDir, "tmp") : path.join(os.tmpdir(), "multica-weixin-host");
}

// One host process owns the state dir, so an in-process queue per file is
// enough to serialize the plugin's read-modify-write cycles.
const queues = new Map();

export async function withFileLock(filePath, _options, fn) {
  const previous = queues.get(filePath) ?? Promise.resolve();
  const run = previous.then(fn, fn);
  const settled = run.catch(() => {});
  queues.set(filePath, settled);
  try {
    return await run;
  } finally {
    if (queues.get(filePath) === settled) queues.delete(filePath);
  }
}
