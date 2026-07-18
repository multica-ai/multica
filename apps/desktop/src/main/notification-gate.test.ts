import { describe, expect, it } from "vitest";
import {
  NotificationGate,
  parseNativeNotificationPayload,
} from "./notification-gate";

describe("NotificationGate", () => {
  it("suppresses native notifications while any app window is focused", () => {
    const gate = new NotificationGate();
    expect(gate.shouldShow("item-1", true)).toBe(false);
  });

  it("shows only the first renderer event for an inbox item", () => {
    const gate = new NotificationGate();
    expect(gate.shouldShow("item-1", false)).toBe(true);
    expect(gate.shouldShow("item-1", false)).toBe(false);
  });

  it("still deduplicates an item when focus suppresses it", () => {
    const gate = new NotificationGate();
    expect(gate.shouldShow("item-1", true)).toBe(false);
    expect(gate.shouldShow("item-1", false)).toBe(false);
  });

  it("bounds remembered ids without blocking newer items", () => {
    const gate = new NotificationGate(2);
    expect(gate.shouldShow("item-1", false)).toBe(true);
    expect(gate.shouldShow("item-2", false)).toBe(true);
    expect(gate.shouldShow("item-3", false)).toBe(true);
    expect(gate.shouldShow("item-1", false)).toBe(true);
  });
});

describe("parseNativeNotificationPayload", () => {
  it("rejects malformed renderer payloads before native side effects", () => {
    expect(parseNativeNotificationPayload(null)).toBeNull();
    expect(
      parseNativeNotificationPayload({
        slug: "acme",
        itemId: "item-1",
        issueKey: "MUL-1",
        title: "New update",
      }),
    ).toBeNull();
  });

  it("accepts the complete bounded payload", () => {
    const payload = {
      slug: "acme",
      itemId: "item-1",
      issueKey: "MUL-1",
      title: "New update",
      body: "A comment was added",
    };
    expect(parseNativeNotificationPayload(payload)).toEqual(payload);
  });

  it("preserves empty optional routing/body fields from legacy events", () => {
    const payload = {
      slug: "",
      itemId: "item-1",
      issueKey: "MUL-1",
      title: "New update",
      body: "",
    };
    expect(parseNativeNotificationPayload(payload)).toEqual(payload);
  });
});
