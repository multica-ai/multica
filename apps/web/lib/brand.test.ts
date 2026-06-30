import { describe, expect, it } from "vitest";
import { WEB_BRAND_METADATA, WEB_BRAND_NAME } from "./brand";

describe("web brand", () => {
  it("uses CoStrict for the Web product identity", () => {
    expect(WEB_BRAND_NAME).toBe("CoStrict");
    expect(JSON.stringify(WEB_BRAND_METADATA)).toContain("CoStrict");
    expect(JSON.stringify(WEB_BRAND_METADATA)).not.toContain("Multica");
  });
});
