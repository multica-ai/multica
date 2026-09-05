// @vitest-environment node
import { describe, expect, it } from "vitest";
import { formatByteSize, hasUnknownTruncation } from "./output-truncation";

// Canonical layer for the pure truncation helpers. The component suites cover
// wiring and rendering; the state matrix lives here.
describe("hasUnknownTruncation", () => {
  it("reports unknown only for tool results with no state", () => {
    expect(
      hasUnknownTruncation([{ type: "tool_result", output_truncated: undefined }]),
    ).toBe(true);
  });

  it("does not treat an explicit false as unknown", () => {
    // The distinction this whole feature rests on: false is a daemon asserting
    // the output is complete, undefined is a daemon that could not say. Reading
    // false as unknown would put a hedging notice on every healthy transcript.
    expect(
      hasUnknownTruncation([{ type: "tool_result", output_truncated: false }]),
    ).toBe(false);
  });

  it("does not treat a truncated result as unknown", () => {
    expect(
      hasUnknownTruncation([{ type: "tool_result", output_truncated: true }]),
    ).toBe(false);
  });

  it("ignores non-tool_result rows", () => {
    // Text and thinking rows never carry the field; counting them would make
    // the notice show on every transcript.
    expect(
      hasUnknownTruncation([
        { type: "text", output_truncated: undefined },
        { type: "thinking", output_truncated: undefined },
        { type: "tool_use", output_truncated: undefined },
      ]),
    ).toBe(false);
  });

  it("reports unknown when any result in a mixed set lacks state", () => {
    expect(
      hasUnknownTruncation([
        { type: "tool_result", output_truncated: true },
        { type: "tool_result", output_truncated: false },
        { type: "tool_result", output_truncated: undefined },
      ]),
    ).toBe(true);
  });

  it("is false for an empty timeline", () => {
    expect(hasUnknownTruncation([])).toBe(false);
  });
});

describe("formatByteSize", () => {
  it.each([
    [0, "0 B"],
    [512, "512 B"],
    [1024, "1 KB"],
    [1536, "2 KB"],
    [1048576, "1.0 MB"],
    [5242880, "5.0 MB"],
  ])("formats %d as %s", (bytes, want) => {
    expect(formatByteSize(bytes)).toBe(want);
  });

  it.each([
    ["negative", -1],
    ["NaN", Number.NaN],
    ["Infinity", Number.POSITIVE_INFINITY],
  ])("returns empty for a %s size", (_label, value) => {
    // A caller renders the badge without a size rather than printing "-1 B".
    expect(formatByteSize(value)).toBe("");
  });
});
