import { describe, expect, it } from "vitest";
import {
  executionLogFilterKey,
  executionLogKeys,
  executionLogPageOptions,
  flattenExecutionLogPages,
} from "./queries";
import type { ExecutionLogPage, TaskMessagePayload } from "../types/events";

function msg(seq: number): TaskMessagePayload {
  return { task_id: "t1", issue_id: "i1", seq, type: "text", content: `m${seq}` };
}

function page(seqs: number[], older: string | null): ExecutionLogPage {
  return {
    messages: seqs.map(msg),
    limit: 50,
    older_cursor: older,
    latest_cursor: "latest",
    raw_total: 100,
    matched_total: 100,
    type_facets: [],
    tool_facets: [],
  };
}

describe("flattenExecutionLogPages", () => {
  it("returns empty for no pages", () => {
    expect(flattenExecutionLogPages(undefined)).toEqual([]);
    expect(flattenExecutionLogPages([])).toEqual([]);
  });

  it("orders oldest→newest by reversing page order (page 0 is the newest window)", () => {
    // Page 0 = newest window [4,5]; page 1 = older [2,3]; page 2 = oldest [1].
    const pages = [page([4, 5], "c1"), page([2, 3], "c2"), page([1], null)];
    expect(flattenExecutionLogPages(pages).map((m) => m.seq)).toEqual([1, 2, 3, 4, 5]);
  });

  it("preserves within-page chronological order for a single page", () => {
    expect(flattenExecutionLogPages([page([1, 2, 3], null)]).map((m) => m.seq)).toEqual([
      1, 2, 3,
    ]);
  });
});

describe("executionLogPageOptions", () => {
  it("threads older_cursor as the next page param and stops when absent", () => {
    const opts = executionLogPageOptions("t1");
    const more = opts.getNextPageParam(page([4, 5], "c1"), [], null, []);
    const done = opts.getNextPageParam(page([1], null), [], null, []);
    expect(more).toBe("c1");
    expect(done).toBeUndefined();
  });

  it("puts task id + filters in the query key, not the cursor", () => {
    const opts = executionLogPageOptions("t1", { types: ["error"] });
    expect(opts.queryKey).toEqual(executionLogKeys.page("t1", executionLogFilterKey({ types: ["error"] })));
    expect(JSON.stringify(opts.queryKey)).not.toContain("c1");
  });

  it("isolates the cache by filter but not by chip order", () => {
    const a = executionLogPageOptions("t1", { types: ["error", "text"] }).queryKey;
    const b = executionLogPageOptions("t1", { types: ["text", "error"] }).queryKey;
    const c = executionLogPageOptions("t1", { types: ["error"] }).queryKey;
    expect(a).toEqual(b); // same chips, different order → same cache
    expect(a).not.toEqual(c); // different selection → distinct cache
  });
});

describe("executionLogFilterKey", () => {
  it("is stable regardless of chip order and separates types from tools", () => {
    expect(executionLogFilterKey({ types: ["b", "a"] })).toBe(
      executionLogFilterKey({ types: ["a", "b"] }),
    );
    expect(executionLogFilterKey({ types: ["x"] })).not.toBe(
      executionLogFilterKey({ tools: ["x"] }),
    );
    expect(executionLogFilterKey(undefined)).toBe(executionLogFilterKey({}));
  });
});
