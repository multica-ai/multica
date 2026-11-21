// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ApiError, parseRetryAfter } from "../api/client";
import { INBOX_TRANSIENT_RETRIES, inboxQueryRetry, inboxQueryRetryDelay, isTransientInboxError } from "./retry";

const status = (code: number, retryAfterMs?: number) => new ApiError("failed", code, "", undefined, retryAfterMs);

describe("isTransientInboxError", () => {
  it.each([408, 429, 502, 503, 504])("treats %i as worth another try", (code) => {
    expect(isTransientInboxError(status(code))).toBe(true);
  });

  it.each([400, 401, 403, 404, 500])("treats %i as an answer", (code) => {
    expect(isTransientInboxError(status(code))).toBe(false);
  });

  it("treats a request that never got an answer as transient, and other errors as not", () => {
    expect(isTransientInboxError(new TypeError("Failed to fetch"))).toBe(true);
    expect(isTransientInboxError(new DOMException("Aborted", "AbortError"))).toBe(false);
    expect(isTransientInboxError(new Error("parse"))).toBe(false);
    expect(isTransientInboxError(null)).toBe(false);
  });
});

describe("inboxQueryRetry", () => {
  it("retries a transient failure a bounded number of times", () => {
    const tries = Array.from({ length: 6 }, (_, n) => inboxQueryRetry(n, status(503)));
    expect(tries.filter(Boolean)).toHaveLength(INBOX_TRANSIENT_RETRIES);
    expect(tries.slice(0, INBOX_TRANSIENT_RETRIES).every(Boolean)).toBe(true);
  });

  it("retries any other failure once, as every other query does", () => {
    expect(inboxQueryRetry(0, status(500))).toBe(true);
    expect(inboxQueryRetry(1, status(500))).toBe(false);
  });
});

describe("inboxQueryRetryDelay", () => {
  const none = () => 0;
  const most = () => 1;

  it("backs off exponentially from one second, capped", () => {
    expect([0, 1, 2, 3, 4, 10].map((n) => inboxQueryRetryDelay(n, status(503), none))).toEqual([
      1000, 2000, 4000, 8000, 8000, 8000,
    ]);
  });

  it("never comes back before Retry-After", () => {
    expect(inboxQueryRetryDelay(0, status(503, 5000), none)).toBe(5000);
    expect(inboxQueryRetryDelay(0, status(503, 60_000), none)).toBe(60_000);
    // A shorter Retry-After does not cut the backoff.
    expect(inboxQueryRetryDelay(2, status(503, 1000), none)).toBe(4000);
  });

  it("spreads clients by up to a quarter of the delay", () => {
    expect(inboxQueryRetryDelay(0, status(503, 1000), most)).toBe(1250);
    expect(inboxQueryRetryDelay(1, status(503), most)).toBe(2500);
  });
});

describe("parseRetryAfter", () => {
  const now = Date.parse("2026-11-19T00:00:00Z");

  it("reads seconds and HTTP dates", () => {
    expect(parseRetryAfter("1", now)).toBe(1000);
    expect(parseRetryAfter(" 0 ", now)).toBe(0);
    expect(parseRetryAfter("Thu, 19 Nov 2026 00:00:03 GMT", now)).toBe(3000);
    expect(parseRetryAfter("Wed, 18 Nov 2026 23:59:00 GMT", now)).toBe(0);
  });

  it("ignores a missing or unreadable header", () => {
    expect(parseRetryAfter(null, now)).toBeUndefined();
    expect(parseRetryAfter("", now)).toBeUndefined();
    expect(parseRetryAfter("soon", now)).toBeUndefined();
    expect(parseRetryAfter("-1", now)).toBeUndefined();
  });
});
