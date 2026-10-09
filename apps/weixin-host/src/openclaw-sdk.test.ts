// @vitest-environment node
import { describe, expect, it } from "vitest";

import { normalizeAccountId } from "openclaw/plugin-sdk/account-id";
import {
  resolveDirectDmAuthorizationOutcome,
  resolveSenderCommandAuthorizationWithRuntime,
} from "openclaw/plugin-sdk/command-auth";

describe("openclaw-sdk shim", () => {
  it("normalizes iLink bot ids to file-safe account ids", () => {
    expect(normalizeAccountId("ABC123@im.bot")).toBe("abc123-im-bot");
  });

  it("only lets allow-listed Weixin users through", async () => {
    const isSenderAllowed = (id: string, list: string[]) => list.includes(id);
    const owner = await resolveSenderCommandAuthorizationWithRuntime({
      senderId: "owner@im.wechat",
      isSenderAllowed,
      readAllowFromStore: async () => ["owner@im.wechat"],
    });
    const stranger = await resolveSenderCommandAuthorizationWithRuntime({
      senderId: "stranger@im.wechat",
      isSenderAllowed,
      readAllowFromStore: async () => ["owner@im.wechat"],
    });

    expect(resolveDirectDmAuthorizationOutcome(owner)).toBe("allowed");
    expect(resolveDirectDmAuthorizationOutcome(stranger)).toBe("unauthorized");
  });

  it("fails closed when nobody is allow-listed", async () => {
    const result = await resolveSenderCommandAuthorizationWithRuntime({
      senderId: "anyone@im.wechat",
      // The plugin's own predicate treats an empty list as "everyone".
      isSenderAllowed: (_id: string, list: string[]) => list.length === 0,
      readAllowFromStore: async () => [],
    });

    expect(resolveDirectDmAuthorizationOutcome(result)).toBe("unauthorized");
  });
});
