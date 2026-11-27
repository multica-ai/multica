// @vitest-environment node
import { describe, expect, it } from "vitest";
import { chatSessionDisplayTitle } from "./chat-session-title";
import { i18n } from "./i18n/singleton";
import type { SupportedLocale } from "./i18n/types";

describe("chatSessionDisplayTitle", () => {
  const display = (locale: SupportedLocale, title: string | null | undefined) =>
    chatSessionDisplayTitle(title, i18n.getFixedT(locale, "chat"));

  it.each([
    ["en", "New chat"],
    ["zh-Hans", "新建聊天"],
    ["zh-Hant", "新建聊天"],
  ] as const)(
    "falls back to the localized New chat for an empty channel-created Chat (%s)",
    (locale, expected) => {
      expect(display(locale, "")).toBe(expected);
      expect(display(locale, null)).toBe(expected);
      expect(display(locale, undefined)).toBe(expected);
    },
  );

  it("preserves a stored or manually renamed title", () => {
    expect(display("en", "Investigate deploy")).toBe("Investigate deploy");
    expect(display("zh-Hans", "Investigate deploy")).toBe("Investigate deploy");
  });
});
