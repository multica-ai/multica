import { describe, expect, it } from "vitest";
import { stripChatQuickActionsProtocol } from "./quick-actions";

describe("stripChatQuickActionsProtocol", () => {
  it("strips complete and partial trailing protocol blocks", () => {
    expect(
      stripChatQuickActionsProtocol(
        "Answer.\n```quick-actions\n[{\"label\":\"Continue\"}]\n```",
      ),
    ).toBe("Answer.");
    expect(
      stripChatQuickActionsProtocol(
        "Answer.\r\n```quick-actions\r\n[{\"label\":",
      ),
    ).toBe("Answer.");
  });

  it("leaves ordinary content untouched", () => {
    expect(stripChatQuickActionsProtocol("Answer with `quick-actions` inline."))
      .toBe("Answer with `quick-actions` inline.");
    expect(
      stripChatQuickActionsProtocol(
        "Example:\n```quick-actions\n[]\n```\nMore explanation.",
      ),
    ).toBe("Example:\n```quick-actions\n[]\n```\nMore explanation.");
  });
});
