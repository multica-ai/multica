// @vitest-environment node
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createUpdaterDiagnostics } from "./updater-diagnostics";

vi.mock("node:fs", async (importOriginal) => {
  const fs = await importOriginal<typeof import("node:fs")>();
  const appendHandles = new Set<number>();
  return {
    ...fs,
    openSync(path: string, flags: string | number, mode: number) {
      const fd = fs.openSync(path, flags, mode);
      const isAppend = typeof flags === "string"
        ? flags.includes("a")
        : (flags & fs.constants.O_APPEND) !== 0;
      if (isAppend) appendHandles.add(fd);
      return fd;
    },
    closeSync(fd: number) {
      appendHandles.delete(fd);
      fs.closeSync(fd);
    },
    ftruncateSync(fd: number, length: number) {
      // Windows does not grant truncation rights to an append-only handle.
      if (appendHandles.has(fd)) throw new Error("Access denied for append-only handle");
      fs.ftruncateSync(fd, length);
    },
  };
});

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

it("replaces a full log and continues recording events", async () => {
  const path = join(directory, "updater.log");
  await writeFile(path, "x".repeat(64 * 1024));
  const record = createUpdaterDiagnostics(directory, "0.11.1");
  record("install-requested");
  record("app-will-quit");
  const log = await readFile(path, "utf8");
  expect(log.length).toBeLessThan(64 * 1024);
  expect(log).toMatch(
    /^\S+ 0\.11\.1 install-requested\n\S+ 0\.11\.1 app-will-quit\n$/,
  );
});

it("does not interrupt the updater if the log path is unavailable", () => {
  const record = createUpdaterDiagnostics(join(directory, "missing"), "0.11.1");
  expect(() => record("startup")).not.toThrow();
});
