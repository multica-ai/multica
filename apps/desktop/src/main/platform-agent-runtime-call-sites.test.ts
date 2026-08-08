import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const daemonManagerPath = [
  resolve(process.cwd(), "src/main/daemon-manager.ts"),
  resolve(process.cwd(), "apps/desktop/src/main/daemon-manager.ts"),
].find(existsSync);
if (!daemonManagerPath) {
  throw new Error("cannot locate Desktop daemon-manager.ts");
}
const daemonManager = readFileSync(daemonManagerPath, "utf8");

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
