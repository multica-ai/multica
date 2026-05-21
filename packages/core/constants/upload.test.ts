import { describe, expect, it } from "vitest";
import { MAX_FILE_SIZE } from "./upload";

describe("upload constants", () => {
  it("caps a single file at 500 MB", () => {
    expect(MAX_FILE_SIZE).toBe(500 * 1024 * 1024);
  });
});
