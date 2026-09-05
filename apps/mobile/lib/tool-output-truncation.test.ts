// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  DISPLAY_CLIP_CHARS,
  TRUNCATION_UNKNOWN_NOTICE,
  clipForDisplay,
  displayClippedLabel,
  formatByteSize,
  hasUnknownTruncation,
  sourceTruncatedLabel,
} from "./tool-output-truncation";

// These rules decide whether a reader is told a tool result is complete. The
// component that renders them holds only JSX, so this file is the canonical
// layer — mobile has no component-test harness, and the recurring defect in
// this area has been wiring, not logic.
//
// Semantics mirror packages/views/common/task-transcript/output-truncation.tsx.

describe("sourceTruncatedLabel", () => {
  it("labels a truncated result with its original size", () => {
    expect(sourceTruncatedLabel({ output_truncated: true, output_original_bytes: 36000 })).toBe(
      "source truncated · 35 KB total",
    );
  });

  it("says nothing when the daemon reports the output is complete", () => {
    // false is a positive assertion, not an absence.
    expect(sourceTruncatedLabel({ output_truncated: false, output_original_bytes: 12 })).toBeNull();
  });

  it("says nothing when the state is unknown", () => {
    // undefined means the reporting daemon could not say. A badge here would
    // assert something nobody knows; the transcript-level notice covers it.
    expect(sourceTruncatedLabel({})).toBeNull();
  });

  it("falls back to a bare label when the size is missing or absurd", () => {
    expect(sourceTruncatedLabel({ output_truncated: true })).toBe("source truncated");
    expect(sourceTruncatedLabel({ output_truncated: true, output_original_bytes: -1 })).toBe(
      "source truncated",
    );
  });

  it("distinguishes source truncation from display clipping in its wording", () => {
    // A collapsed body is not lost data. Conflating the two told users a
    // long-but-complete result had been cut.
    const label = sourceTruncatedLabel({ output_truncated: true, output_original_bytes: 2048 });
    expect(label).toContain("source truncated");
    expect(displayClippedLabel("x".repeat(DISPLAY_CLIP_CHARS + 1))).toContain("Showing the first");
  });
});

describe("displayClippedLabel and clipForDisplay", () => {
  it("says nothing for output that fits", () => {
    expect(displayClippedLabel("short")).toBeNull();
    expect(clipForDisplay("short")).toBe("short");
  });

  it("says nothing at exactly the limit", () => {
    const exact = "x".repeat(DISPLAY_CLIP_CHARS);
    expect(displayClippedLabel(exact)).toBeNull();
    expect(clipForDisplay(exact)).toBe(exact);
  });

  it("clips and explains one byte over the limit", () => {
    const over = "x".repeat(DISPLAY_CLIP_CHARS + 1);
    expect(displayClippedLabel(over)).toBe(
      `Showing the first ${DISPLAY_CLIP_CHARS} characters of the stored preview.`,
    );
    expect(clipForDisplay(over)).toHaveLength(DISPLAY_CLIP_CHARS);
  });
});

describe("hasUnknownTruncation", () => {
  it("reports unknown only for tool results with no state", () => {
    expect(hasUnknownTruncation([{ type: "tool_result" }])).toBe(true);
  });

  it("does not treat an explicit false as unknown", () => {
    expect(hasUnknownTruncation([{ type: "tool_result", output_truncated: false }])).toBe(false);
  });

  it("does not treat a truncated result as unknown", () => {
    expect(hasUnknownTruncation([{ type: "tool_result", output_truncated: true }])).toBe(false);
  });

  it("ignores rows that never carry the field", () => {
    // Counting these would put the notice on every timeline.
    expect(
      hasUnknownTruncation([{ type: "text" }, { type: "thinking" }, { type: "tool_use" }]),
    ).toBe(false);
  });

  it("reports unknown when any result in a mixed set lacks state", () => {
    expect(
      hasUnknownTruncation([
        { type: "tool_result", output_truncated: true },
        { type: "tool_result", output_truncated: false },
        { type: "tool_result" },
      ]),
    ).toBe(true);
  });

  it("is false for an empty timeline", () => {
    expect(hasUnknownTruncation([])).toBe(false);
  });

  it("has a notice to show", () => {
    expect(TRUNCATION_UNKNOWN_NOTICE).toContain("before truncation was tracked");
  });
});

describe("formatByteSize", () => {
  it.each([
    [0, "0 B"],
    [512, "512 B"],
    [1024, "1 KB"],
    [36000, "35 KB"],
    [1048576, "1.0 MB"],
  ])("formats %d as %s", (bytes, want) => {
    expect(formatByteSize(bytes)).toBe(want);
  });

  it.each([
    ["negative", -1],
    ["NaN", Number.NaN],
    ["Infinity", Number.POSITIVE_INFINITY],
  ])("returns empty for a %s size", (_label, value) => {
    expect(formatByteSize(value)).toBe("");
  });
});
