import { afterEach, describe, expect, it } from "vitest";
import { mkdtempSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  writeFreezeBreadcrumb,
  readFreezeBreadcrumb,
  ackFreezeBreadcrumb,
  clearFreezeBreadcrumb,
  FREEZE_BREADCRUMB_TTL_MS,
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

// A `now` safely inside the TTL window relative to sample.ts.
const NOW = sample.ts + 60_000;

describe("freeze breadcrumb read/ack split", () => {
  it("writes then reads back the breadcrumb", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    expect(readFreezeBreadcrumb(file, NOW)).toEqual(sample);
  });

  it("read does NOT clear the file — a re-hang before reporting keeps the retry alive", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    expect(readFreezeBreadcrumb(file, NOW)).toEqual(sample);
    expect(existsSync(file)).toBe(true);
    // A second boot (this one hung before acking) reads it again.
    expect(readFreezeBreadcrumb(file, NOW)).toEqual(sample);
  });

  it("ack with the matching ts deletes the file", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    ackFreezeBreadcrumb(file, sample.ts);
    expect(existsSync(file)).toBe(false);
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
  });

  it("ack with a mismatched ts keeps the file — a late ack can't delete a newer breadcrumb", () => {
    const file = tempFile();
    const newer: FreezeBreadcrumb = { ...sample, ts: sample.ts + 1 };
    writeFreezeBreadcrumb(file, newer);
    // Renderer acks the ts it read BEFORE the newer breadcrumb was written.
    ackFreezeBreadcrumb(file, sample.ts);
    expect(readFreezeBreadcrumb(file, NOW)).toEqual(newer);
  });

  it("ack on a missing file is a no-op, never throws", () => {
    expect(() => ackFreezeBreadcrumb(tempFile(), sample.ts)).not.toThrow();
  });

  it("clearFreezeBreadcrumb removes a pending breadcrumb (hang recovered)", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    clearFreezeBreadcrumb(file);
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
  });
});

describe("freeze breadcrumb TTL", () => {
  it("an expired breadcrumb is dropped AND deleted — no unbounded retry on analytics-disabled builds", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    const afterTtl = sample.ts + FREEZE_BREADCRUMB_TTL_MS + 1;
    expect(readFreezeBreadcrumb(file, afterTtl)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("a breadcrumb exactly at the TTL boundary is still reported", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    const atTtl = sample.ts + FREEZE_BREADCRUMB_TTL_MS;
    expect(readFreezeBreadcrumb(file, atTtl)).toEqual(sample);
    expect(existsSync(file)).toBe(true);
  });
});

// The breadcrumb crosses a process boundary (main writes, renderer flushes via
// IPC) and lives across app versions — a future write shape or a corrupt file
// must never throw into boot. And now that a valid read no longer deletes,
// every invalid payload MUST be deleted on read, or it becomes permanent
// boot noise re-parsed on every launch.
describe("freeze breadcrumb defends against malformed input", () => {
  it("returns null when no file exists", () => {
    expect(readFreezeBreadcrumb(tempFile(), NOW)).toBeNull();
  });

  it("drops and deletes corrupt JSON", () => {
    const file = tempFile();
    writeFileSync(file, "{ not valid json", "utf8");
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("drops and deletes a payload missing `kind`", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ ts: NOW, version: "x" }), "utf8");
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("drops and deletes a payload with a wrong-typed `kind`", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ kind: 42, ts: NOW, context: {} }), "utf8");
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("drops and deletes a payload missing a numeric `ts` — TTL and ack need it", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ kind: "unresponsive", context: {} }), "utf8");
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("drops and deletes a payload with a non-finite `ts`", () => {
    const file = tempFile();
    writeFileSync(
      file,
      JSON.stringify({ kind: "unresponsive", ts: "yesterday", context: {} }),
      "utf8",
    );
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("drops and deletes a JSON null payload", () => {
    const file = tempFile();
    writeFileSync(file, "null", "utf8");
    expect(readFreezeBreadcrumb(file, NOW)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("ack deletes a malformed file too — it could never be reported anyway", () => {
    const file = tempFile();
    writeFileSync(file, "{ not valid json", "utf8");
    ackFreezeBreadcrumb(file, 123);
    expect(existsSync(file)).toBe(false);
  });

  it("clearing a non-existent file is a no-op, never throws", () => {
    expect(() => clearFreezeBreadcrumb(tempFile())).not.toThrow();
  });
});
