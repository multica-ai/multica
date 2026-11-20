// @vitest-environment node
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, it } from "vitest";
import { createUpdaterDiagnostics } from "./updater-diagnostics";

let directory: string;

beforeEach(async () => {
  directory = await mkdtemp(join(tmpdir(), "multica-updater-log-"));
});
afterEach(async () => {
  await rm(directory, { recursive: true });
});

it("preserves the prior launch and writes diagnostics when called", async () => {
  createUpdaterDiagnostics(directory, "0.11.1")("update-downloaded");
  createUpdaterDiagnostics(directory, "0.11.2")("startup");
  const log = await readFile(join(directory, "updater.log"), "utf8");
  expect(log).toMatch(/0\.11\.1 update-downloaded\n.*0\.11\.2 startup\n/s);
});

it("replaces an oversized log on the next write", async () => {
  const path = join(directory, "updater.log");
  await writeFile(path, "x".repeat(64 * 1024));
  createUpdaterDiagnostics(directory, "0.11.1")("install-requested");
  expect(await readFile(path, "utf8")).toMatch(/^\S+ 0\.11\.1 install-requested\n$/);
});

it("does not interrupt the updater if the log path is unavailable", () => {
  const record = createUpdaterDiagnostics(join(directory, "missing"), "0.11.1");
  expect(() => record("startup")).not.toThrow();
});
