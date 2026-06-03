import { describe, it, expect } from "vitest";
import { parseWithFallback } from "./schema";
import { ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES } from "./projects-schema";

describe("ListProjectUpdatesResponseSchema", () => {
  it("returns fallback when updates array is missing", () => {
    const result = parseWithFallback({}, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, { endpoint: "test" });
    expect(result).toEqual(EMPTY_PROJECT_UPDATES);
  });
  it("returns fallback when updates is not an array", () => {
    const result = parseWithFallback({ updates: "notarray" }, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, { endpoint: "test" });
    expect(result).toEqual(EMPTY_PROJECT_UPDATES);
  });
  it("returns fallback when an update has an unknown health value", () => {
    const bad = { updates: [{ id: "1", project_id: "p", workspace_id: "w", health: "green", body: "x", author_type: "member", author_id: "a", created_at: "t", updated_at: "t" }], total: 1 };
    const result = parseWithFallback(bad, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, { endpoint: "test" });
    expect(result).toEqual(EMPTY_PROJECT_UPDATES);
  });
  it("passes a well-formed response through", () => {
    const good = { updates: [{ id: "1", project_id: "p", workspace_id: "w", health: "on_track", body: "ok", author_type: "member", author_id: "a", created_at: "t", updated_at: "t" }], total: 1 };
    const result = parseWithFallback(good, ListProjectUpdatesResponseSchema, EMPTY_PROJECT_UPDATES, { endpoint: "test" });
    expect(result.updates).toHaveLength(1);
    expect(result.updates[0]?.health).toBe("on_track");
  });
});
