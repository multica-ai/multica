import { describe, it, expect } from "vitest";
import {
  DEFAULT_CHAT_PAGE_SETTINGS,
  readChatPageSettings,
} from "./chat-page-settings";

describe("readChatPageSettings", () => {
  it("returns defaults for a missing or non-object blob", () => {
    expect(readChatPageSettings(undefined)).toEqual(DEFAULT_CHAT_PAGE_SETTINGS);
    expect(readChatPageSettings(null)).toEqual(DEFAULT_CHAT_PAGE_SETTINGS);
    expect(readChatPageSettings("nope")).toEqual(DEFAULT_CHAT_PAGE_SETTINGS);
  });

  it("keeps every valid field", () => {
    const stored = {
      limit: 25,
      sort: "name",
      unreadFirst: false,
      groupBy: "none",
      searchDefaultOpen: true,
    };
    expect(readChatPageSettings(stored)).toEqual(stored);
  });

  it("repairs each malformed field back to its default, keeping the good ones", () => {
    const s = readChatPageSettings({
      limit: -3, // invalid → default
      sort: "sideways", // invalid → default
      unreadFirst: "yes", // invalid → default
      groupBy: "none", // valid → kept
      searchDefaultOpen: true, // valid → kept
    });
    expect(s).toEqual({
      limit: DEFAULT_CHAT_PAGE_SETTINGS.limit,
      sort: DEFAULT_CHAT_PAGE_SETTINGS.sort,
      unreadFirst: DEFAULT_CHAT_PAGE_SETTINGS.unreadFirst,
      groupBy: "none",
      searchDefaultOpen: true,
    });
  });

  it("accepts 0 as 'show all' for limit", () => {
    expect(readChatPageSettings({ limit: 0 }).limit).toBe(0);
  });
});
