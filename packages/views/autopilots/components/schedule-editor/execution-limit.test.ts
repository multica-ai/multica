// @vitest-environment node
import { describe, it, expect } from "vitest";
import { validExecutionLimit } from "./execution-limit";

describe("schedule execution count bounds", () => {
  it.each([null, 1, 2, 2147483647])("accepts %s", (v) => expect(validExecutionLimit(v)).toBe(true));
  it.each([0, -1, 1.5, 2147483648, NaN, Infinity])("rejects %s", (v) =>
    expect(validExecutionLimit(v)).toBe(false),
  );
});
