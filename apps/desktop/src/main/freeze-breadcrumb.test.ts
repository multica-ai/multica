import { afterEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  writeFreezeBreadcrumb,
  readAndClearFreezeBreadcrumb,
  clearFreezeBreadcrumb,
  type FreezeBreadcrumb,
} from "./freeze-breadcrumb";

// Each test gets its own temp dir so the on-disk breadcrumb is isolated.
const dirs: string[] = [];
function tempFile(): string {
  const dir = mkdtempSync(join(tmpdir(), "freeze-breadcrumb-"));
  dirs.push(dir);
  return join(dir, "last-client-failure.json");
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

const sample: FreezeBreadcrumb = {
  kind: "unresponsive",
  context: { desktopRoute: { path: "/acme/issues" } },
  ts: 1_700_000_000_000,
  version: "0.3.1",
};

describe("freeze breadcrumb round-trip", () => {
  it("writes then reads back the breadcrumb", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    expect(readAndClearFreezeBreadcrumb(file)).toEqual(sample);
  });

  it("read clears the file so a failure reports exactly once", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    expect(readAndClearFreezeBreadcrumb(file)).toEqual(sample);
    expect(existsSync(file)).toBe(false);
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("clearFreezeBreadcrumb removes a pending breadcrumb (hang recovered)", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    clearFreezeBreadcrumb(file);
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("only lets the window that wrote a breadcrumb clear it", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, { ...sample, ownerId: "issue:2" });

    clearFreezeBreadcrumb(file, "main:1");
    expect(existsSync(file)).toBe(true);

    clearFreezeBreadcrumb(file, "issue:2");
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });
});

// The breadcrumb crosses a process boundary (main writes, renderer flushes via
// IPC) and lives across app versions — a future write shape or a corrupt file
// must never throw into boot. CLAUDE.md "API Response Compatibility".
describe("freeze breadcrumb defends against malformed input", () => {
  it("returns null when no file exists", () => {
    expect(readAndClearFreezeBreadcrumb(tempFile())).toBeNull();
  });

  it("returns null on corrupt JSON", () => {
    const file = tempFile();
    writeFileSync(file, "{ not valid json", "utf8");
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("returns null when `kind` is missing", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ ts: 1, version: "x" }), "utf8");
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("returns null when `kind` is the wrong type", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ kind: 42, context: {} }), "utf8");
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("returns null on a JSON null payload", () => {
    const file = tempFile();
    writeFileSync(file, "null", "utf8");
    expect(readAndClearFreezeBreadcrumb(file)).toBeNull();
  });

  it("clearing a non-existent file is a no-op, never throws", () => {
    expect(() => clearFreezeBreadcrumb(tempFile())).not.toThrow();
  });
});
