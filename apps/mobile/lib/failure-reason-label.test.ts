// @vitest-environment node
import { describe, expect, it } from "vitest";

import chatEn from "../locales/en/chat.json";
import chatZh from "../locales/zh-Hans/chat.json";
import { failureReasonKey, REASONS } from "./failure-reason-label";
describe("failureReasonLabel", () => {
  it("maps known reasons to i18n keys", () => {
    expect(failureReasonKey("runtime_access_denied")).toBe(
      "failure_reason.runtime_access_denied",
    );
    expect(
      failureReasonKey("agent_error.provider_auth_or_access"),
    ).toBe("failure_reason.agent_error_provider_auth_or_access");
  });

  it("falls back to a user-friendly unknown reason", () => {
    expect(failureReasonKey("not_known_yet")).toBe(
      "failure_reason.default",
    );
    expect(failureReasonKey(null)).toBe("failure_reason.default");
  });

  // The REASONS set and the locale bundles drifted once before: keys existed
  // in the locales but not in the set, so real runs fell through to the
  // generic "Run failed" copy. Pin the two together.
  it("covers exactly the failure_reason keys in the locale bundles", () => {
    const localeKeys = (bundle: { failure_reason: Record<string, string> }) =>
      new Set(
        Object.keys(bundle.failure_reason).map((key) => `failure_reason.${key}`),
      );
    const enKeys = localeKeys(chatEn);
    const zhKeys = localeKeys(chatZh);

    expect(zhKeys).toEqual(enKeys);

    const setKeys = new Set([...REASONS].map((reason) => failureReasonKey(reason)));
    const expected = new Set(
      [...enKeys].filter((key) => key !== "failure_reason.default"),
    );
    expect(setKeys).toEqual(expected);
  });
});
