// @vitest-environment node
import type { TFunction } from "i18next";
import { createI18n } from "@multica/core/i18n/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { describe, expect, it } from "vitest";
import enAgents from "../../../locales/en/agents.json";
import jaAgents from "../../../locales/ja/agents.json";
import koAgents from "../../../locales/ko/agents.json";
import zhHansAgents from "../../../locales/zh-Hans/agents.json";

import {
  FAILURE_REASON_I18N_KEYS,
  cancelReasonLabel,
  failureReasonLabel,
} from "./task-failure";

const AGENT_RESOURCES = {
  en: enAgents,
  "zh-Hans": zhHansAgents,
  ja: jaAgents,
  ko: koAgents,
} as const;

function fixedT(locale: SupportedLocale): TFunction<"agents"> {
  const resources =
    locale === "en"
      ? { en: { agents: enAgents } }
      : {
          en: { agents: enAgents },
          [locale]: { agents: AGENT_RESOURCES[locale] },
        };
  const i18n = createI18n(locale, resources);
  return i18n.getFixedT(locale, "agents") as TFunction<"agents">;
}

const enT = fixedT("en");

// cancelReasonLabel decides which cancelled rows explain themselves. The rule
// it must hold: a SERVER-cancelled row (persisted reason) reads like a failed
// row, a user's own cancel stays a plain "Cancelled" — labelling every cancel
// would bury the rows that actually need the user to act.
describe("cancelReasonLabel", () => {
  it("returns null for a user-initiated cancel", () => {
    expect(
      cancelReasonLabel(
        { status: "cancelled", error: null, failure_reason: null },
        enT,
      ),
    ).toBeNull();
  });

  it("only ever labels cancelled rows — failed rows have their own path", () => {
    expect(
      cancelReasonLabel(
        { status: "failed", error: "boom", failure_reason: "timeout" },
        enT,
      ),
    ).toBeNull();
  });

  it("labels the worktree claim gate's cancellation", () => {
    expect(
      cancelReasonLabel(
        {
          status: "cancelled",
          error: "worktree mode needs daemon version 0.4.24 or newer",
          failure_reason: "local_directory_error",
        },
        enT,
      ),
    ).toBe("Local directory error");
  });

  it("localizes a generic system cancellation in every supported locale", () => {
    const expected: Record<SupportedLocale, string> = {
      en: "Cancelled by the system",
      "zh-Hans": "系统已取消",
      ja: "システムによってキャンセルされました",
      ko: "시스템에서 취소함",
    };

    for (const locale of Object.keys(expected) as SupportedLocale[]) {
      expect(
        cancelReasonLabel(
          {
            status: "cancelled",
            error: "work preserved in the worktree at /env/worktree",
            failure_reason: null,
          },
          fixedT(locale),
        ),
      ).toBe(expected[locale]);
    }
  });
});

describe("failureReasonLabel", () => {
  it("renders every known reason through i18n in every supported locale", () => {
    for (const locale of Object.keys(AGENT_RESOURCES) as SupportedLocale[]) {
      const t = fixedT(locale);
      for (const reason of Object.keys(FAILURE_REASON_I18N_KEYS)) {
        const label = failureReasonLabel(reason, t);
        expect(label, `${locale}: ${reason}`).not.toBe(reason);
        expect(label, `${locale}: ${reason}`).not.toContain("task_failure.");
        expect(label, `${locale}: ${reason}`).not.toBe("");
      }
    }
  });

  it("covers platform reasons added after the original hardcoded map", () => {
    expect(failureReasonLabel("invalid_task_identity", enT)).toBe(
      "Task identity mismatch",
    );
    expect(failureReasonLabel("issue_window_restricted", enT)).toBe(
      "Issue unavailable under workspace limits",
    );
  });

  it("covers operational reasons emitted outside the canonical taxonomy", () => {
    expect(failureReasonLabel("agent_fallback_message", enT)).toBe(
      "Agent returned a fallback message",
    );
    expect(failureReasonLabel("idle_watchdog", enT)).toBe(
      "Agent stopped after inactivity",
    );
    expect(failureReasonLabel("cancelled", enT)).toBe(
      "Cancelled by the system",
    );
  });

  it("localizes the legacy user cancellation value retained by the service", () => {
    expect(failureReasonLabel("user_cancelled", enT)).toBe(
      "Cancelled by user",
    );
  });

  it("uses native copy for representative refined reasons", () => {
    expect(
      failureReasonLabel(
        "agent_error.provider_quota_limit",
        fixedT("zh-Hans"),
      ),
    ).toBe("提供商配额已用尽");
    expect(
      failureReasonLabel("agent_error.context_overflow", fixedT("ja")),
    ).toBe("コンテキストウィンドウを超過しました");
    expect(
      failureReasonLabel("agent_error.missing_config", fixedT("ko")),
    ).toBe("API 키 또는 설정 누락");
  });

  it("still falls back to the raw wire value for unknown reasons", () => {
    for (const locale of Object.keys(AGENT_RESOURCES) as SupportedLocale[]) {
      expect(failureReasonLabel("brand_new_reason", fixedT(locale))).toBe(
        "brand_new_reason",
      );
    }
  });

  it.each(["constructor", "toString", "__proto__"])(
    "treats inherited Object property %s as an unknown wire value",
    (reason) => {
      expect(failureReasonLabel(reason, enT)).toBe(reason);
    },
  );

  it("returns null when the server supplied no reason", () => {
    expect(failureReasonLabel(null, enT)).toBeNull();
    expect(failureReasonLabel(undefined, enT)).toBeNull();
    expect(failureReasonLabel("", enT)).toBeNull();
  });
});
