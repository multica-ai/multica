import { describe, expect, it } from "vitest";
import { applyFixPath } from "./fix-path";

describe("applyFixPath", () => {
  it("uses the default function exported by CommonJS interop", () => {
    let called = false;

    applyFixPath({
      default: () => {
        called = true;
      },
    });

    expect(called).toBe(true);
  });
});
