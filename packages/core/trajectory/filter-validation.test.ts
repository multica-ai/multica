// @vitest-environment node
import { expect, it } from "vitest";
import { parseGraphPropertyNumber, validGraphPropertyFilters, validGraphPropertyValue } from "./filter-validation";

it.each([
  ["1_000", 1000], ["1_000.5", 1000.5], ["0x1p4", 16], ["-0x_1.f_fp+4", -31.9375],
  [".1_25e+2", 12.5], ["0x.8p1", 1], ["0x1.fffffffffffffp1023", Number.MAX_VALUE],
  ["0x1p-1074", Number.MIN_VALUE], ["0x1fffffffffffffp-1126", 1e-323],
  [`0x1${"0".repeat(300)}p-1200`, 1],
] as const)("parses finite Go property number %s without losing its value", (value, expected) => {
  expect(parseGraphPropertyNumber(value)).toBe(expected);
});
it.each(["NaN", "Infinity", "1e309", "0x1p1024", "0x1.fffffffffffff8p1023", "0x10", "1__0", "1_", "_1", "0x1p_4", " 3 ", ""])("rejects nonfinite or invalid numeric syntax %s", value => {
  expect(parseGraphPropertyNumber(value)).toBeUndefined();
});
const members = (n: number, value = "literal") => Array.from({ length: n }, () => value);
it("enforces property definitions and per-definition members independently of alternative expansion", () => {
  expect(validGraphPropertyFilters(Object.fromEntries(Array.from({ length: 20 }, (_, n) => [String(n), ["value"]])))).toBe(true);
  expect(validGraphPropertyFilters(Object.fromEntries(Array.from({ length: 21 }, (_, n) => [String(n), ["value"]])))).toBe(false);
  expect(validGraphPropertyFilters({ p: members(50) })).toBe(true);
  expect(validGraphPropertyFilters({ p: members(51) })).toBe(false);
});
it("counts exact string, boolean, native-number, operator and repeated unset alternatives like the server", () => {
  expect(validGraphPropertyFilters({ p1: members(50), p2: members(50), p3: members(28) })).toBe(true);
  expect(validGraphPropertyFilters({ p1: members(50), p2: members(50), p3: members(29) })).toBe(false);
  const base = { p1: members(50, "true"), p2: [...members(35, "0x1p4"), { op: "contains" as const, value: "one" }] };
  expect(validGraphPropertyFilters(base)).toBe(true);
  expect(validGraphPropertyFilters({ ...base, p3: [{ op: "contains", value: "two" }] })).toBe(false);
  expect(validGraphPropertyFilters({ ...base, p3: members(50, "__none__") })).toBe(false);
  expect(validGraphPropertyFilters({ p1: members(50), p2: members(50), p3: members(27), p4: members(50, "__none__") })).toBe(true);
});
it("bounds contains by Unicode code points in flat and advanced property members", () => {
  expect(validGraphPropertyValue({ op: "contains", value: "🦊".repeat(2000) })).toBe(true);
  expect(validGraphPropertyValue({ op: "contains", value: "🦊".repeat(2001) })).toBe(false);
  expect(validGraphPropertyValue({ op: "contains", value: "  " })).toBe(true);
});
