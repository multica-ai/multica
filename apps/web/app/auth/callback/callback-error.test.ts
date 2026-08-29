// @vitest-environment node

import { describe, expect, it } from "vitest";
import { callbackErrorFrom } from "./callback-error";

describe("callbackErrorFrom", () => {
  it.each([
    "account_disabled",
    "signup_prohibited",
    "email_not_allowed",
    "google_account_no_email",
    "oauth_code_invalid",
  ] as const)("keeps the stable %s error kind for localization", (code) => {
    expect(callbackErrorFrom(code, "English fallback")).toEqual({ kind: code });
  });

  it("keeps an actionable message from an older server that returned an uncoded 4xx", () => {
    expect(callbackErrorFrom(undefined, "registration is disabled")).toEqual({
      kind: "raw",
      text: "registration is disabled",
    });
  });

  it("hides 5xx and transport details behind the localized generic failure", () => {
    expect(callbackErrorFrom(undefined, undefined)).toEqual({
      kind: "login_failed",
    });
  });
});
