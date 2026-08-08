import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const daemonManager = readFileSync(
  fileURLToPath(new URL("./daemon-manager.ts", import.meta.url)),
  "utf8",
);

function sourceBetween(start: string, end: string): string {
  const startIndex = daemonManager.indexOf(start);
  const endIndex = daemonManager.indexOf(end, startIndex + start.length);
  expect(startIndex, `${start} must exist`).toBeGreaterThan(-1);
  expect(endIndex, `${end} must follow ${start}`).toBeGreaterThan(startIndex);
  return daemonManager.slice(startIndex, endIndex);
}

describe("Desktop-owned daemon Platform Agent environment", () => {
  it("uses the bundled runtime environment for the runtime probe child", () => {
    const probe = sourceBetween(
      "async function probeLocalRuntimes",
      "function desktopSpawnEnv",
    );
    expect(probe).toContain("env: desktopSpawnEnv()");
  });

  it("uses the bundled runtime environment for the daemon start child", () => {
    const start = sourceBetween(
      "async function startDaemon",
      "function stopDaemon",
    );
    expect(start).toContain("env: desktopSpawnEnv()");
  });
});
