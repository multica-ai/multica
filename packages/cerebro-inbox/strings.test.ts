import { describe, expect, it } from "vitest";
import { pickCerebroInboxStrings } from "./strings";

// JEH-1322 regression — zh-Hans previously fell back to the English table
// because only `da` and `en` branches existed, surfacing an "Unarchive"
// aria-label on archived row actions under Chinese locale.
describe("pickCerebroInboxStrings", () => {
  it("returns the zh-Hans table for zh-Hans", () => {
    const s = pickCerebroInboxStrings("zh-Hans");
    expect(s.unarchive_label).toBe("取消归档");
    expect(s.archive_label).toBe("归档");
  });

  it("returns the zh-Hans table for zh-CN and bare zh", () => {
    expect(pickCerebroInboxStrings("zh-CN").unarchive_label).toBe("取消归档");
    expect(pickCerebroInboxStrings("zh").unarchive_label).toBe("取消归档");
  });

  it("returns the Danish table for da / da-DK", () => {
    expect(pickCerebroInboxStrings("da").unarchive_label).toBe("Gendan");
    expect(pickCerebroInboxStrings("da-DK").unarchive_label).toBe("Gendan");
    expect(pickCerebroInboxStrings("da-DK").mute).toBe("Påmind mig");
  });

  it("falls back to English for unsupported locales and missing language", () => {
    expect(pickCerebroInboxStrings("fr").unarchive_label).toBe("Unarchive");
    expect(pickCerebroInboxStrings(undefined).unarchive_label).toBe("Unarchive");
  });

  // FIR-1645 — the Archived block's second restore action ("unarchive & mark
  // unread") is localized in every table, never silently English-only.
  it("localizes the unarchive-and-mark-unread label per locale", () => {
    expect(pickCerebroInboxStrings("en").unarchive_unread_label).toBe("Unarchive & mark unread");
    expect(pickCerebroInboxStrings("da").unarchive_unread_label).toBe("Gendan & marker som ulæst");
    expect(pickCerebroInboxStrings("zh-Hans").unarchive_unread_label).toBe("取消归档并标为未读");
  });
});
