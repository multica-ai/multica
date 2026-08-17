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

// `now` inside the TTL for `sample`, so the age check isn't what a case is
// accidentally exercising.
const fresh = sample.ts + 1_000;

describe("freeze breadcrumb round-trip", () => {
  it("writes then reads back the breadcrumb", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    expect(readFreezeBreadcrumb(file, fresh)).toEqual(sample);
  });

  it("clearFreezeBreadcrumb removes a pending breadcrumb (hang recovered)", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    clearFreezeBreadcrumb(file);
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  it("only lets the window that wrote a breadcrumb clear it", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, { ...sample, ownerId: "issue:2" });

    clearFreezeBreadcrumb(file, "main:1");
    expect(existsSync(file)).toBe(true);

    clearFreezeBreadcrumb(file, "issue:2");
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });
});

// The whole point of splitting read from delete: a deterministic freeze means
// the user reopens the app and hits the same content again seconds later. If
// reading consumed the breadcrumb, that second hang would take the still-queued
// event with it and the failure would never be reported (MUL-4115).
describe("freeze breadcrumb survives an undelivered report", () => {
  it("reading does not consume the breadcrumb", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);

    expect(readFreezeBreadcrumb(file, fresh)).toEqual(sample);
    expect(existsSync(file)).toBe(true);
    // A boot that died before acknowledging still finds it next time.
    expect(readFreezeBreadcrumb(file, fresh)).toEqual(sample);
  });

  it("ack with the matching ts retires the breadcrumb", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);

    ackFreezeBreadcrumb(file, sample.ts);

    expect(existsSync(file)).toBe(false);
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  it("a late ack does not delete a newer failure recorded since the read", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);
    readFreezeBreadcrumb(file, fresh);

    // Second hang overwrites the single slot before the first report acks.
    const newer: FreezeBreadcrumb = { ...sample, ts: sample.ts + 5_000 };
    writeFreezeBreadcrumb(file, newer);
    ackFreezeBreadcrumb(file, sample.ts);

    expect(readFreezeBreadcrumb(file, newer.ts + 1_000)).toEqual(newer);
  });

  it("ack on a missing file is a no-op, never throws", () => {
    expect(() => ackFreezeBreadcrumb(tempFile(), 1)).not.toThrow();
  });

  it("drops a breadcrumb past the TTL instead of retrying it forever", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);

    const expired = sample.ts + FREEZE_BREADCRUMB_TTL_MS + 1;
    expect(readFreezeBreadcrumb(file, expired)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("keeps a breadcrumb that is exactly at the TTL boundary", () => {
    const file = tempFile();
    writeFreezeBreadcrumb(file, sample);

    const boundary = sample.ts + FREEZE_BREADCRUMB_TTL_MS;
    expect(readFreezeBreadcrumb(file, boundary)).toEqual(sample);
  });
});

// The breadcrumb crosses a process boundary (main writes, renderer flushes via
// IPC) and lives across app versions — a future write shape or a corrupt file
// must never throw into boot. CLAUDE.md "API Response Compatibility".
describe("freeze breadcrumb defends against malformed input", () => {
  it("returns null when no file exists", () => {
    expect(readFreezeBreadcrumb(tempFile(), fresh)).toBeNull();
  });

  it("returns null on corrupt JSON", () => {
    const file = tempFile();
    writeFileSync(file, "{ not valid json", "utf8");
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  it("returns null when `kind` is missing", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ ts: 1, version: "x" }), "utf8");
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  it("returns null when `kind` is the wrong type", () => {
    const file = tempFile();
    writeFileSync(file, JSON.stringify({ kind: 42, context: {} }), "utf8");
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  it("returns null on a JSON null payload", () => {
    const file = tempFile();
    writeFileSync(file, "null", "utf8");
    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
  });

  // A malformed payload can never be acknowledged (the renderer never gets a
  // ts for it), so it must be discarded on read or it becomes permanent boot
  // noise.
  it("deletes a malformed payload rather than retrying it every boot", () => {
    const file = tempFile();
    writeFileSync(file, "{ not valid json", "utf8");

    readFreezeBreadcrumb(file, fresh);

    expect(existsSync(file)).toBe(false);
  });

  it("drops a breadcrumb with a non-numeric ts (unackable)", () => {
    const file = tempFile();
    writeFileSync(
      file,
      JSON.stringify({ kind: "unresponsive", context: {}, ts: "nope" }),
      "utf8",
    );

    expect(readFreezeBreadcrumb(file, fresh)).toBeNull();
    expect(existsSync(file)).toBe(false);
  });

  it("clearing a non-existent file is a no-op, never throws", () => {
    expect(() => clearFreezeBreadcrumb(tempFile())).not.toThrow();
  });
});
