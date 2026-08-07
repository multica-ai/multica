import { describe, expect, it } from "vitest";
import { buildMobileLoginUrl, getAuthHandoffToken } from "./auth-handoff";

describe("mobile auth handoff", () => {
  it("builds the web login URL without losing an existing path", () => {
    expect(buildMobileLoginUrl("https://multica.example/base")).toBe(
      "https://multica.example/login?platform=mobile",
    );
  });

  it("accepts only the registered Multica auth callback", () => {
    expect(
      getAuthHandoffToken("multica://auth/callback?token=signed-jwt"),
    ).toBe("signed-jwt");
    expect(
      getAuthHandoffToken("multica://settings/callback?token=signed-jwt"),
    ).toBeNull();
    expect(
      getAuthHandoffToken("https://auth/callback?token=signed-jwt"),
    ).toBeNull();
  });
});
