import { describe, expect, it } from "vitest";
import { isUnauthenticatedExposureBlocked } from "./access-guard";

describe("isUnauthenticatedExposureBlocked", () => {
  it("blocks a production run by default", () => {
    expect(isUnauthenticatedExposureBlocked({ NODE_ENV: "production" })).toBe(true);
  });

  it("does not block development", () => {
    expect(isUnauthenticatedExposureBlocked({ NODE_ENV: "development" })).toBe(false);
  });

  it("does not block test", () => {
    expect(isUnauthenticatedExposureBlocked({ NODE_ENV: "test" })).toBe(false);
  });

  it("allows an explicit opt-in in production", () => {
    expect(
      isUnauthenticatedExposureBlocked({
        NODE_ENV: "production",
        ADMIN_ALLOW_UNSAFE_NO_AUTH: "true",
      }),
    ).toBe(false);
  });

  it("does not treat a truthy-but-not-'true' value as opt-in", () => {
    expect(
      isUnauthenticatedExposureBlocked({
        NODE_ENV: "production",
        ADMIN_ALLOW_UNSAFE_NO_AUTH: "1",
      }),
    ).toBe(true);
  });
});
