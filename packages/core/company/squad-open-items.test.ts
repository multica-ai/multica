// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const listIssues = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { listIssues } }));

import { SQUAD_OPEN_ITEMS_LIMIT, squadOpenItemsOptions } from "./squad-open-items";

describe("squadOpenItemsOptions", () => {
  beforeEach(() => listIssues.mockReset());

  it("asks for the squad's unfinished issues, capped, and keeps the total", async () => {
    listIssues.mockResolvedValueOnce({ issues: [{ id: "a" }], total: 27 });
    const opts = squadOpenItemsOptions("ws-1", "squad-9");

    const result = await (opts.queryFn as unknown as () => Promise<unknown>)();

    expect(listIssues).toHaveBeenCalledWith({
      assignee_id: "squad-9",
      assignee_types: ["squad"],
      open_only: true,
      limit: SQUAD_OPEN_ITEMS_LIMIT,
    });
    expect(result).toEqual({ issues: [{ id: "a" }], total: 27 });
  });

  it("is keyed by workspace and squad", () => {
    expect(squadOpenItemsOptions("ws-1", "squad-9").queryKey).toEqual(
      expect.arrayContaining(["ws-1", "squad-open-items", "squad-9"]),
    );
  });
});
