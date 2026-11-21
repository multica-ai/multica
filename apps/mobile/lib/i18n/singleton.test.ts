// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";

import { i18n } from "./singleton";

describe("mobile i18n singleton", () => {
  afterEach(async () => {
    await i18n.changeLanguage("en");
  });

  it("loads the English and Simplified Chinese bundles", async () => {
    await i18n.changeLanguage("en");
    expect(i18n.t("actions.cancel")).toBe("Cancel");
    expect(i18n.t("common:actions.cancel")).toBe("Cancel");
    expect(i18n.t("navigation:tabs.inbox")).toBe("Inbox");

    await i18n.changeLanguage("zh-Hans");
    expect(i18n.t("actions.cancel")).toBe("取消");
    expect(i18n.t("common:actions.cancel")).toBe("取消");
    expect(i18n.t("navigation:tabs.inbox")).toBe("收件箱");
  });

  it("formats counts with interpolation", async () => {
    await i18n.changeLanguage("zh-Hans");
    expect(i18n.t("issues:list.count_other", { count: 3 })).toBe(
      "3 个任务",
    );
  });

  it("resolves plural base keys for count=1 and count=2 across en, zh-Hans, and zh-Hant", async () => {
    await i18n.changeLanguage("en");
    expect(i18n.t("issues:jump_new", { count: 1 })).toBe("1 new");
    expect(i18n.t("issues:jump_new", { count: 2 })).toBe("2 new");
    expect(i18n.t("issues:jump_a11y", { count: 1 })).toBe("Jump to 1 new message");
    expect(i18n.t("issues:jump_a11y", { count: 2 })).toBe("Jump to 2 new messages");
    expect(
      i18n.t("issues:resolved.summary", { count: 1, authors: "Alice" }),
    ).toBe("Resolved · 1 message by Alice");
    expect(
      i18n.t("issues:resolved.summary", { count: 2, authors: "Alice" }),
    ).toBe("Resolved · 2 messages by Alice");

    await i18n.changeLanguage("zh-Hans");
    expect(i18n.t("issues:jump_new", { count: 1 })).toBe("1 条新消息");
    expect(i18n.t("issues:jump_new", { count: 2 })).toBe("2 条新消息");
    expect(i18n.t("issues:jump_a11y", { count: 1 })).toBe("跳到 1 条新消息");
    expect(
      i18n.t("issues:resolved.summary", { count: 1, authors: "Alice" }),
    ).toBe("已解决 · 1 条消息，由 Alice 处理");
    expect(
      i18n.t("issues:resolved.a11y_folded", { count: 1, authors: "Alice" }),
    ).toBe("由 Alice 解决的讨论，1 条消息。点按展开。");

    await i18n.changeLanguage("zh-Hant");
    expect(i18n.t("issues:jump_new", { count: 1 })).toBe("1 則新訊息");
    expect(i18n.t("issues:jump_new", { count: 2 })).toBe("2 則新訊息");
    expect(i18n.t("issues:jump_a11y", { count: 1 })).toBe("跳到 1 條新訊息");
    expect(
      i18n.t("issues:resolved.summary", { count: 1, authors: "Alice" }),
    ).toBe("已解決 · 1 條訊息，由 Alice 處理");
    expect(
      i18n.t("issues:resolved.a11y_folded", { count: 1, authors: "Alice" }),
    ).toBe("由 Alice 解決的討論，1 條訊息。點按展開。");
  });

  it("renders both issue statuses and run statuses translated in Chinese", async () => {
    await i18n.changeLanguage("zh-Hans");
    expect(i18n.t("issues:status.todo")).toBe("待办");
    expect(i18n.t("issues:status.in_review")).toBe("审核中");
    expect(i18n.t("issues:status.cancelled")).toBe("已取消");
    expect(i18n.t("issues:runs.status.queued")).toBe("排队中");
    expect(i18n.t("issues:runs.status.completed")).toBe("运行成功");
  });
});
