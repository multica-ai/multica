import { describe, expect, it } from "vitest";
import { AUTO_SUBSCRIBE_DEFAULTS, getAutoSubscribe } from "./auto-subscribe";

describe("getAutoSubscribe", () => {
  it("returns the default when prefs is undefined", () => {
    expect(getAutoSubscribe(undefined, "creator")).toBe(true);
    expect(getAutoSubscribe(undefined, "assignee")).toBe(true);
    expect(getAutoSubscribe(undefined, "mentioned")).toBe(false);
    expect(getAutoSubscribe(undefined, "commenter")).toBe(false);
  });

  it("returns the default when auto_subscribe block is missing", () => {
    expect(getAutoSubscribe({ notifications: { mentioned: "off" } }, "mentioned")).toBe(false);
  });

  it("returns the default when the reason key is missing", () => {
    expect(getAutoSubscribe({ auto_subscribe: { creator: false } }, "mentioned")).toBe(false);
    expect(getAutoSubscribe({ auto_subscribe: { creator: false } }, "creator")).toBe(false);
  });

  it("returns the default when the value is not a boolean", () => {
    expect(getAutoSubscribe({ auto_subscribe: { commenter: "yes" } }, "commenter")).toBe(false);
    expect(getAutoSubscribe({ auto_subscribe: { commenter: 1 } }, "commenter")).toBe(false);
  });

  it("respects an explicit override", () => {
    expect(getAutoSubscribe({ auto_subscribe: { mentioned: true } }, "mentioned")).toBe(true);
    expect(getAutoSubscribe({ auto_subscribe: { creator: false } }, "creator")).toBe(false);
  });

  it("exposes defaults that match the server contract", () => {
    expect(AUTO_SUBSCRIBE_DEFAULTS).toEqual({
      creator: true,
      assignee: true,
      mentioned: false,
      commenter: false,
    });
  });
});
