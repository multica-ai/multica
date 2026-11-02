// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { BriefPushPreferenceSchema, EMPTY_BRIEF_PUSH } from "./brief-push-schema";

const parse = (body: unknown) =>
  parseWithFallback(body, BriefPushPreferenceSchema, EMPTY_BRIEF_PUSH, { endpoint: "t" });

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
