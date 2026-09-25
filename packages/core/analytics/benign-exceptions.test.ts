import { describe, expect, it } from "vitest";
import { isBenignException } from "./benign-exceptions";

describe("isBenignException", () => {
  it("drops ResizeObserver loop errors via $exception_list value", () => {
    expect(
      isBenignException({
        $exception_list: [
          {
            type: "Error",
            value: "ResizeObserver loop completed with undelivered notifications.",
          },
        ],
      }),
    ).toBe(true);
  });

  it("drops the older 'loop limit exceeded' phrasing", () => {
    expect(
      isBenignException({
        $exception_list: [
          { type: "Error", value: "ResizeObserver loop limit exceeded" },
        ],
      }),
    ).toBe(true);
  });

  it("drops when the signal is on the top-level $exception_message", () => {
    expect(
      isBenignException({
        $exception_message: "ResizeObserver loop limit exceeded",
      }),
    ).toBe(true);
  });

  it("matches case-insensitively", () => {
    expect(
      isBenignException({ $exception_message: "resizeobserver LOOP limit exceeded" }),
    ).toBe(true);
  });

  it("keeps real errors", () => {
    expect(
      isBenignException({
        $exception_list: [
          {
            type: "TypeError",
            value: "Cannot read properties of undefined (reading 'split')",
          },
        ],
      }),
    ).toBe(false);
  });

  it("does not match an unrelated mention of ResizeObserver", () => {
    // Only the benign "loop" phrasing is silenced; a genuine bug in
    // ResizeObserver usage must still be reported.
    expect(
      isBenignException({
        $exception_message: "ResizeObserver is not defined",
      }),
    ).toBe(false);
  });

  it("fails open on missing or malformed properties", () => {
    expect(isBenignException(undefined)).toBe(false);
    expect(isBenignException({})).toBe(false);
    expect(isBenignException({ $exception_list: "not-an-array" })).toBe(false);
    expect(isBenignException({ $exception_list: [null, 42, {}] })).toBe(false);
    expect(isBenignException({ $exception_message: 123 })).toBe(false);
  });
});
