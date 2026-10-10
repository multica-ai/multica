// @vitest-environment node
import { expect, it } from "vitest";
import { beginChatSend, hasChatSendInFlight } from "./chat-send-lifecycle";

it("tracks overlapping request lifetimes per session and finishes each lease only once", () => {
  const first = beginChatSend("A");
  const second = beginChatSend("A");
  const otherSession = beginChatSend("B");
  first();
  first();
  expect(hasChatSendInFlight("A")).toBe(true);
  otherSession();
  expect(hasChatSendInFlight("B")).toBe(false);
  expect(hasChatSendInFlight("A")).toBe(true);
  second();
  expect(hasChatSendInFlight("A")).toBe(false);
  expect(hasChatSendInFlight(null)).toBe(false);
});
