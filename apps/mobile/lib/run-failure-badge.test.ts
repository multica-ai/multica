import { describe, expect, it } from "vitest";

import { i18n } from "./i18n/singleton";
import { runFailureBadgeLabel } from "./run-failure-badge";

const tEn = i18n.getFixedT("en", "issues");

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

  it("names a deleted project folder in the reader's language", () => {
    expect(runFailureBadgeLabel("local_directory_missing", tEn)).toBe(
      "Folder missing",
    );
    expect(
      runFailureBadgeLabel(
        "local_directory_missing",
        i18n.getFixedT("zh-Hans", "issues"),
      ),
    ).toBe("文件夹不存在");
    expect(
      runFailureBadgeLabel(
        "local_directory_missing",
        i18n.getFixedT("zh-Hant", "issues"),
      ),
    ).toBe("資料夾不存在");
  });

  it("keeps the pre-MUL-1949 coarse values readable", () => {
    expect(runFailureBadgeLabel("agent_error", tEn)).toBe("Agent error");
    expect(runFailureBadgeLabel("manual", tEn)).toBe("Manual");
  });

  it("degrades to no badge for a reason newer than this build", () => {
    // The row renders a bare status word instead. Unlike web, a compact badge
    // has no room for the raw wire value, so silence is the fallback — but it
    // must be a miss on THIS map, not a value the parser threw away upstream.
    expect(runFailureBadgeLabel("some_future_reason", tEn)).toBeUndefined();
    expect(runFailureBadgeLabel(undefined, tEn)).toBeUndefined();
    expect(runFailureBadgeLabel("", tEn)).toBeUndefined();
  });
});
