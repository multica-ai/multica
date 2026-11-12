// @vitest-environment node
import { describe, expect, it } from "vitest";
import { offscreenPins, sameIds, pinBodyVisible } from "./comment-pins";
import type { TimelineEntry } from "@multica/core/types";

const entry = (id: string, pinned_at: string | null, extra = {}): TimelineEntry => ({ type: "comment", id, actor_type: "member", actor_id: "u", created_at: "2026-01-01", pinned_at, ...extra });
describe("comment pin navigation", () => {
 it("orders newest pins first with a deterministic tie break and excludes visible, deleted and non-comment rows", () => {
  const rows = [entry("a", "2026-01-01"), entry("c", "2026-02-01"), entry("b", "2026-02-01"), entry("v", "2026-03-01"), entry("d", "2026-04-01", { deleted_at: "today" }), entry("x", null), entry("activity", "2026-05-01", { type: "activity" })];
  expect(offscreenPins(rows, new Set(["v"])).map(e => e.id)).toEqual(["b", "c", "a"]);
  expect(offscreenPins(rows, new Set(["a", "b", "c", "v"]))).toEqual([]);
 });
 it("compares sets independently of insertion order", () => {
  expect(sameIds(new Set(["a", "b"]), new Set(["b", "a"]))).toBe(true);
  expect(sameIds(new Set(["a"]), new Set(["b"]))).toBe(false);
 });
 it("requires a real body intersection below the fixed occlusion reserve", () => {
  expect(pinBodyVisible({ top: 10, bottom: 40, height: 30 }, { top: 0, bottom: 500 })).toBe(false);
  expect(pinBodyVisible({ top: 90, bottom: 190, height: 100 }, { top: 0, bottom: 500 })).toBe(true);
  expect(pinBodyVisible({ top: 550, bottom: 650, height: 100 }, { top: 0, bottom: 500 })).toBe(false);
 });
 it("treats content under a sticky bottom overlay as hidden", () => {
  expect(pinBodyVisible({ top: 400, bottom: 450, height: 50 }, { top: 0, bottom: 500 })).toBe(true);
  expect(pinBodyVisible({ top: 400, bottom: 450, height: 50 }, { top: 0, bottom: 500 }, 150)).toBe(false);
  expect(pinBodyVisible({ top: 300, bottom: 450, height: 150 }, { top: 0, bottom: 500 }, 150)).toBe(true);
 });
});
