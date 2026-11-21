import { describe, expect, it } from "vitest";

import issuesEn from "../locales/en/issues.json";
import issuesZhHans from "../locales/zh-Hans/issues.json";
import issuesZhHant from "../locales/zh-Hant/issues.json";
import { i18n } from "./i18n/singleton";
import {
  FAILURE_BADGE_REASONS,
  failureBadgeKey,
  runFailureBadgeLabel,
} from "./run-failure-badge";

const tEn = i18n.getFixedT("en", "issues");
const tZhHans = i18n.getFixedT("zh-Hans", "issues");
const tZhHant = i18n.getFixedT("zh-Hant", "issues");

describe("runFailureBadgeLabel", () => {
  it("names the reasons the backend classifies today", () => {
    expect(runFailureBadgeLabel("agent_error.provider_network", tEn)).toBe(
      "Network error",
    );
    expect(runFailureBadgeLabel("skill_bundle_unavailable", tEn)).toBe(
      "Skill download failed",
    );
    // #7913: the run died setting up its environment on the executing
    // machine. The badge has to say so — "Failed" alone is what sent the
    // original report's diagnosis at a model provider.
    expect(runFailureBadgeLabel("environment_prepare_failed", tEn)).toBe(
      "Environment setup failed",
    );
    expect(runFailureBadgeLabel("runtime_access_denied", tEn)).toBe(
      "No runtime access",
    );
  });

  it("names the reason in the reader's language", () => {
    expect(runFailureBadgeLabel("local_directory_missing", tEn)).toBe(
      "Folder missing",
    );
    expect(runFailureBadgeLabel("local_directory_missing", tZhHans)).toBe(
      "文件夹不存在",
    );
    expect(runFailureBadgeLabel("local_directory_missing", tZhHant)).toBe(
      "資料夾不存在",
    );

    expect(
      runFailureBadgeLabel("agent_error.provider_capacity_or_rate_limit", tZhHans),
    ).toBe("触发限流");
    expect(
      runFailureBadgeLabel("agent_error.provider_capacity_or_rate_limit", tZhHant),
    ).toBe("觸發限流");
    expect(runFailureBadgeLabel("runtime_access_denied", tZhHans)).toBe(
      "无运行时权限",
    );
    expect(runFailureBadgeLabel("runtime_access_denied", tZhHant)).toBe(
      "無執行時權限",
    );
  });

  it("keeps the pre-MUL-1949 coarse values readable", () => {
    expect(runFailureBadgeLabel("agent_error", tEn)).toBe("Agent error");
    expect(runFailureBadgeLabel("manual", tEn)).toBe("Manual");
    expect(runFailureBadgeLabel("manual", tZhHans)).toBe("手动取消");
    expect(runFailureBadgeLabel("manual", tZhHant)).toBe("手動取消");
  });

  it("degrades to no badge for a reason newer than this build", () => {
    // The row renders a bare status word instead. Unlike web, a compact badge
    // has no room for the raw wire value, so silence is the fallback — but it
    // must be a miss on THIS map, not a value the parser threw away upstream.
    for (const t of [tEn, tZhHans, tZhHant]) {
      expect(runFailureBadgeLabel("some_future_reason", t)).toBeUndefined();
      expect(runFailureBadgeLabel(undefined, t)).toBeUndefined();
      expect(runFailureBadgeLabel("", t)).toBeUndefined();
    }
  });

  // Read the raw bundles rather than going through `t`: i18next falls back to
  // English, which would hide a reason missing from a Chinese bundle.
  it.each([
    ["en", issuesEn],
    ["zh-Hans", issuesZhHans],
    ["zh-Hant", issuesZhHant],
  ])("translates every known reason in %s, and nothing else", (_locale, bundle) => {
    const badges: Record<string, string> = bundle.runs.failure_badge;
    const expected = [...FAILURE_BADGE_REASONS].map(failureBadgeKey).sort();
    expect(Object.keys(badges).sort()).toEqual(expected);
    for (const key of expected) {
      expect(badges[key], key).toMatch(/\S/);
    }
  });
});
