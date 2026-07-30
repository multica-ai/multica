/**
 * Frames are rebuilt at both ends — capture and egress — because the on-disk
 * breadcrumb between them is barely validated. These cases pin what "rebuilt"
 * means: four scalars, and nothing that could be dereferenced into user data.
 */
import { describe, expect, it } from "vitest";

import {
  HANG_STACK_MAX_FRAMES,
  redactFrameUrl,
  sanitizeHangStackFrames,
} from "./hang-stack";

const cdpFrame = {
  functionName: "parseMarkdownChunked",
  url: "file:///Applications/Multica.app/Contents/Resources/app.asar/out/renderer/assets/index-abc.js",
  location: { lineNumber: 412, columnNumber: 17 },
  // Present on every real paused frame; both dereference into live values.
  scopeChain: [{ type: "local", object: { objectId: "{secret-scope}" } }],
  this: { objectId: "{secret-this}" },
};

describe("sanitizeHangStackFrames", () => {
  it("keeps the four scalar fields and reduces the url", () => {
    expect(sanitizeHangStackFrames([cdpFrame])).toEqual([
      {
        functionName: "parseMarkdownChunked",
        url: "assets/index-abc.js",
        lineNumber: 412,
        columnNumber: 17,
      },
    ]);
  });

  it("drops scope handles that could be dereferenced into user data", () => {
    const frames = sanitizeHangStackFrames([cdpFrame]);

    expect(JSON.stringify(frames)).not.toContain("secret");
    expect(frames?.[0]).not.toHaveProperty("scopeChain");
    expect(frames?.[0]).not.toHaveProperty("this");
  });

  it("drops any key a future protocol version might add", () => {
    const frames = sanitizeHangStackFrames([
      { ...cdpFrame, functionLocation: { scriptId: "7" }, returnValue: "raw-value" },
    ]);

    expect(JSON.stringify(frames)).not.toContain("raw-value");
    expect(frames?.[0]).not.toHaveProperty("returnValue");
  });

  // A breadcrumb frame has already been flattened; a CDP frame nests position
  // under `location`. Re-sanitizing an already-sanitized frame must be lossless
  // or the egress-side rebuild would zero out every line number.
  it("accepts an already-flattened frame without losing its position", () => {
    const flattened = {
      functionName: "setContent",
      url: "assets/index-abc.js",
      lineNumber: 90,
      columnNumber: 4,
    };

    expect(sanitizeHangStackFrames([flattened])).toEqual([flattened]);
    expect(sanitizeHangStackFrames(sanitizeHangStackFrames([cdpFrame]))).toEqual(
      sanitizeHangStackFrames([cdpFrame]),
    );
  });

  it("labels an anonymous frame rather than dropping it", () => {
    expect(sanitizeHangStackFrames([{ functionName: "", location: {} }])).toEqual([
      { functionName: "(anonymous)", url: "", lineNumber: 0, columnNumber: 0 },
    ]);
  });

  it("returns null for a missing or empty frame list", () => {
    expect(sanitizeHangStackFrames(undefined)).toBeNull();
    expect(sanitizeHangStackFrames([])).toBeNull();
    expect(sanitizeHangStackFrames("not-an-array")).toBeNull();
  });

  it("keeps the top of the stack when it is deeper than the cap", () => {
    const deep = Array.from({ length: 50 }, (_, i) => ({
      ...cdpFrame,
      functionName: `fn${i}`,
    }));

    expect(sanitizeHangStackFrames(deep, 3).map((f) => f!.functionName)).toEqual([
      "fn0",
      "fn1",
      "fn2",
    ]);
    expect(sanitizeHangStackFrames(deep)).toHaveLength(HANG_STACK_MAX_FRAMES);
  });

  it("skips a non-object entry instead of failing the whole stack", () => {
    const frames = sanitizeHangStackFrames([null, cdpFrame, "junk"]);
    expect(frames).toHaveLength(1);
  });
});

describe("redactFrameUrl", () => {
  it("strips the install path, which can contain the OS username", () => {
    expect(
      redactFrameUrl("file:///Users/someone/Applications/Multica.app/out/renderer/main.js"),
    ).toBe("renderer/main.js");
  });

  it("is idempotent, so re-sanitizing does not erode the value", () => {
    const once = redactFrameUrl(
      "file:///Users/someone/Applications/Multica.app/out/renderer/main.js",
    );
    expect(redactFrameUrl(once)).toBe(once);
  });

  it("drops query and hash", () => {
    expect(redactFrameUrl("assets/index.js?v=1#L2")).toBe("assets/index.js");
  });

  it("tolerates a frame with no url", () => {
    expect(redactFrameUrl(undefined)).toBe("");
    expect(redactFrameUrl("")).toBe("");
  });
});
