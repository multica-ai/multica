// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  BriefPushBindingListSchema,
  BriefPushPreferenceSchema,
  EMPTY_BRIEF_PUSH,
} from "./brief-push-schema";

const parse = (body: unknown) =>
  parseWithFallback(body, BriefPushPreferenceSchema, EMPTY_BRIEF_PUSH, { endpoint: "t" });

const parseBindings = (body: unknown) =>
  parseWithFallback(body, BriefPushBindingListSchema, [], { endpoint: "t" });

describe("BriefPushPreferenceSchema", () => {
  it("reads a saved preference with its chat and quiet hours", () => {
    expect(
      parse({ brief_push: true, binding_id: "b1", quiet_hours: { start: 22, end: 7 } }),
    ).toEqual({ brief_push: true, binding_id: "b1", quiet_hours: { start: 22, end: 7 } });
  });

  it("reads the server default for a member who never set one", () => {
    expect(parse({ brief_push: false, binding_id: null, quiet_hours: null })).toEqual(EMPTY_BRIEF_PUSH);
  });

  it("fills the defaults for fields an older server omits", () => {
    expect(parse({})).toEqual(EMPTY_BRIEF_PUSH);
  });

  it("reads a malformed quiet_hours as none and keeps the rest", () => {
    expect(parse({ brief_push: true, binding_id: "b1", quiet_hours: { start: 25, end: 7 } })).toEqual({
      brief_push: true,
      binding_id: "b1",
      quiet_hours: null,
    });
    expect(parse({ brief_push: true, binding_id: "b1", quiet_hours: "night" })).toMatchObject({
      brief_push: true,
      quiet_hours: null,
    });
  });

  it("treats a non-boolean brief_push as off", () => {
    expect(parse({ brief_push: "yes", binding_id: "b1", quiet_hours: null })).toMatchObject({ brief_push: false });
  });

  it("falls back to the default preference when the body is not an object", () => {
    expect(parse("garbage")).toEqual(EMPTY_BRIEF_PUSH);
    expect(parse(null)).toEqual(EMPTY_BRIEF_PUSH);
  });
});

describe("BriefPushBindingListSchema", () => {
  it("reads the caller's chats", () => {
    expect(
      parseBindings([{ id: "b1", channel: "telegram", label: "Ops room" }]),
    ).toEqual([{ id: "b1", channel: "telegram", label: "Ops room" }]);
  });

  it("reads an empty list when no chat is linked", () => {
    expect(parseBindings([])).toEqual([]);
  });

  it("drops an entry without an id or channel and keeps the others", () => {
    expect(
      parseBindings([
        { channel: "telegram", label: "No id" },
        { id: "b2", label: "No channel" },
        { id: "", channel: "telegram", label: "Empty id" },
        { id: "b3", channel: "telegram", label: "Good" },
      ]),
    ).toEqual([{ id: "b3", channel: "telegram", label: "Good" }]);
  });

  it("reads a malformed label as empty and keeps the entry", () => {
    expect(parseBindings([{ id: "b1", channel: "telegram", label: 42 }])).toEqual([
      { id: "b1", channel: "telegram", label: "" },
    ]);
  });

  it("drops a non-object entry without losing the list", () => {
    expect(parseBindings(["b1", null, { id: "b2", channel: "telegram", label: "Kept" }])).toEqual([
      { id: "b2", channel: "telegram", label: "Kept" },
    ]);
  });

  it("falls back to an empty list when the body is not an array", () => {
    expect(parseBindings({ bindings: [{ id: "b1", channel: "telegram", label: "x" }] })).toEqual([]);
    expect(parseBindings("garbage")).toEqual([]);
    expect(parseBindings(null)).toEqual([]);
  });
});
