// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { CommentSchema, TimelineEntriesSchema, EMPTY_COMMENT } from "./schemas";
import { commentToTimelineEntry } from "../issues/comment-thread";

const pin = { pinned_at: "2026-11-12T08:00:00Z", pinned_by_type: "member", pinned_by_id: "ffb691cd-3270-4268-bfde-9cccf87c5312" } as const;
const comment = { ...EMPTY_COMMENT, id: "comment", issue_id: "issue", ...pin };
afterEach(() => vi.unstubAllGlobals());
describe("comment pins API compatibility", () => {
 it("preserves pins through comments, conversion and timeline parsing", () => {
  const parsed = CommentSchema.parse(comment);
  expect(parsed).toMatchObject(pin);
  expect(TimelineEntriesSchema.parse([commentToTimelineEntry(comment)])[0]).toMatchObject(pin);
 });
 it.each([{ pinned_at: 123 }, { pinned_at: "not a date" }, { pinned_by_type: {} }, { pinned_by_id: "bad" }, { pinned_by_type: null }])("drops invalid pin metadata without dropping content: %j", invalid => {
  expect(CommentSchema.parse({ ...comment, ...invalid })).toMatchObject({ id: "comment", pinned_at: null });
  expect(TimelineEntriesSchema.parse([{ ...commentToTimelineEntry(comment), ...invalid }])).toMatchObject([{ id: "comment", pinned_at: null }]);
 });
 it("accepts old servers with no pin fields", () => expect(CommentSchema.parse(EMPTY_COMMENT).pinned_at).toBeNull());
 it.each(["pinComment", "unpinComment"] as const)("%s uses the pin endpoint and degrades malformed responses", async method => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ bad: true }), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  const api = new ApiClient("https://api.example.test");
  expect(await api[method]("comment")).toEqual(EMPTY_COMMENT);
  expect(fetch.mock.calls[0]![0]).toBe("https://api.example.test/api/comments/comment/pin");
  expect(fetch.mock.calls[0]![1].method).toBe(method === "pinComment" ? "POST" : "DELETE");
 });
});
