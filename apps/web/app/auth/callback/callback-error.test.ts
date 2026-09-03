// @vitest-environment node

import { describe, expect, it } from "vitest";
import { ApiError } from "@multica/core/api";
import { callbackErrorFrom } from "./callback-error";

describe("callbackErrorFrom", () => {
  it.each([
    "account_disabled",
    "signup_prohibited",
    "email_not_allowed",
    "google_account_no_email",
    "oauth_code_invalid",
  ] as const)("keeps the stable %s error kind for localization", (code) => {
    const err = new ApiError("English fallback", 403, "Forbidden", { code });
    expect(callbackErrorFrom(err)).toEqual({ kind: code });
  });

  it("keeps an actionable message from an older server that returned an uncoded 4xx", () => {
    const err = new ApiError("registration is disabled", 403, "Forbidden");
    expect(callbackErrorFrom(err)).toEqual({
      kind: "raw",
      text: "registration is disabled",
    });
  });

  it("preserves unknown actionable codes as their server message", () => {
    const err = new ApiError("new signup restriction", 403, "Forbidden", {
      code: "future_restriction",
    });
    expect(callbackErrorFrom(err)).toEqual({
      kind: "raw",
      text: "new signup restriction",
    });
  });

  it("localizes a known 4xx code even without a fallback message", () => {
    const err = new ApiError("", 400, "Bad Request", { code: "oauth_code_invalid" });
    expect(callbackErrorFrom(err)).toEqual({ kind: "oauth_code_invalid" });
  });

  it.each([undefined, null, {}, { code: 42 }, { code: "" }])(
    "handles malformed error bodies: %j",
    (body) => {
      const err = new ApiError("actionable fallback", 400, "Bad Request", body);
      expect(callbackErrorFrom(err)).toEqual({ kind: "raw", text: "actionable fallback" });
    },
  );

  it.each([500, 502, 503])(
    "hides %s details even when a known code is present",
    (status) => {
      const err = new ApiError("internal detail", status, "Server Error", {
        code: "oauth_code_invalid",
      });
      expect(callbackErrorFrom(err)).toEqual({ kind: "login_failed" });
    },
  );

  it.each([new Error("Failed to fetch"), null, undefined, { code: "oauth_code_invalid" }])(
    "handles transport and non-API errors: %j",
    (err) => {
      expect(callbackErrorFrom(err)).toEqual({ kind: "login_failed" });
    },
  );

  it("uses the localized generic failure for an empty, uncoded 4xx", () => {
    expect(callbackErrorFrom(new ApiError("", 400, "Bad Request"))).toEqual({
      kind: "login_failed",
    });
  });
});
