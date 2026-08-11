import { describe, expect, it } from "vitest";
import { isFeedbackContext } from "./types";

describe("isFeedbackContext", () => {
  it("accepts desktop route error context", () => {
    expect(
      isFeedbackContext({
        kind: "desktop_route_error",
        trigger: "route-errorElement",
        error: {
          name: "TypeError",
          message: "Cannot read properties of undefined",
          stack: "TypeError: Cannot read properties of undefined",
        },
      }),
    ).toBe(true);
  });

  it("rejects malformed context", () => {
    expect(
      isFeedbackContext({
        kind: "desktop_route_error",
        trigger: "route-errorElement",
        error: { name: "TypeError" },
      }),
    ).toBe(false);
    expect(isFeedbackContext({ kind: "unknown" })).toBe(false);
  });
});
