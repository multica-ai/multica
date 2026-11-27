// @vitest-environment node
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import chatEn from "../locales/en/chat.json";
import chatZh from "../locales/zh-Hans/chat.json";
import { failureReasonKey, REASONS } from "./failure-reason-label";
import { i18n } from "./i18n/singleton";
import { runFailureBadgeLabel } from "./run-failure-badge";

const tEn = i18n.getFixedT("en", "issues");

describe("failureReasonKey", () => {
  it("maps known reasons to i18n keys", () => {
    expect(failureReasonKey("runtime_access_denied")).toBe(
      "failure_reason.runtime_access_denied",
    );
    expect(
      failureReasonKey("agent_error.provider_auth_or_access"),
    ).toBe("failure_reason.agent_error_provider_auth_or_access");
  });

  it("explains revoked access with a rebind action", () => {
    expect(failureReasonKey("runtime_access_revoked")).toBe(
      "failure_reason.runtime_access_revoked",
    );
    expect(chatEn.failure_reason.runtime_access_revoked).toBe(
      "Runtime access was revoked. Bind the agent to an accessible runtime before starting another run.",
    );
    expect(runFailureBadgeLabel("runtime_access_revoked", tEn)).toBe("Access revoked");
  });

  // The server's reason list is a fixture the Go test
  // TestClientFailureReasonFixture keeps current. Read from disk, not
  // imported: mobile code does not depend on packages/views.
  it("has a label for every reason the server taxonomy defines", () => {
    const serverReasons: string[] = JSON.parse(
      readFileSync(
        new URL(
          "../../../packages/views/agents/components/tabs/test-fixtures/server-failure-reasons.json",
          import.meta.url,
        ),
        "utf8",
      ),
    );
    expect(serverReasons.filter((reason) => !REASONS.has(reason))).toEqual([]);
  });

  it("falls back to a user-friendly unknown reason", () => {
    expect(failureReasonKey("not_known_yet")).toBe(
      "failure_reason.default",
    );
    expect(failureReasonKey(null)).toBe("failure_reason.default");
    expect(runFailureBadgeLabel("not_known_yet", tEn)).toBeUndefined();
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
