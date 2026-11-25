import { describe, expect, it } from "vitest";
import { isStorageLimitReached } from "./upload-error";

// The shape of `ApiError` in data/api.ts, which pulls in native modules.
function apiError(status: number, body: unknown) {
  return Object.assign(new Error("refused"), { status, body });
}

describe("isStorageLimitReached", () => {
  it("recognises the storage refusal", () => {
    expect(isStorageLimitReached(apiError(403, { code: "storage_limit_reached", used_bytes: 1 }))).toBe(true);
  });

  it("ignores other failures", () => {
    expect(isStorageLimitReached(apiError(413, { code: "storage_limit_reached" }))).toBe(false);
    expect(isStorageLimitReached(apiError(403, { code: "feature_locked" }))).toBe(false);
    expect(isStorageLimitReached(new Error("storage_limit_reached"))).toBe(false);
    expect(isStorageLimitReached(null)).toBe(false);
  });
});
