// @vitest-environment node
import { describe, expect, it } from "vitest";
import { imageDimensions } from "./image-metadata";

describe("imageDimensions", () => {
  it("keeps a valid pair", () => {
    expect(imageDimensions({ width: 2560, height: 1720 })).toEqual({ width: 2560, height: 1720 });
  });
  it.each([undefined, null, "3200", 0, -1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])(
    "rejects invalid or incomplete dimensions: %s",
    (width) => {
      expect(imageDimensions({ width: width as number, height: 2000 })).toBeUndefined();
      expect(imageDimensions({ width: 3200, height: width as number })).toBeUndefined();
    },
  );
});
