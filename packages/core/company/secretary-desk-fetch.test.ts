// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "../types";

const listIssues = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { listIssues } }));

import { secretaryDeskDirectivesOptions } from "./secretary-desk";

function rows(count: number, start = 0): Issue[] {
  return Array.from({ length: count }, (_, i) => ({ id: `i${start + i}` }) as unknown as Issue);
}

async function runDirectives(): Promise<Issue[]> {
  const opts = secretaryDeskDirectivesOptions("ws-1");
  return (opts.queryFn as unknown as () => Promise<Issue[]>)();
}

describe("secretaryDeskDirectivesOptions", () => {
  beforeEach(() => listIssues.mockReset());

  it("reads every page so rows past the first 100 are not dropped", async () => {
    listIssues
      .mockResolvedValueOnce({ issues: rows(100, 0), total: 150 })
      .mockResolvedValueOnce({ issues: rows(50, 100), total: 150 });

    const issues = await runDirectives();

    expect(issues).toHaveLength(150);
    expect(listIssues.mock.calls[0]?.[0]).toMatchObject({ origin_type: "directive", limit: 100, offset: 0 });
    expect(listIssues.mock.calls[1]?.[0]).toMatchObject({ origin_type: "directive", limit: 100, offset: 100 });
  });

  it("stops after one request when everything fits in a page", async () => {
    listIssues.mockResolvedValueOnce({ issues: rows(3), total: 3 });
    expect(await runDirectives()).toHaveLength(3);
    expect(listIssues).toHaveBeenCalledTimes(1);
  });

  it("stops on an empty page even if the total says more remain", async () => {
    listIssues.mockResolvedValueOnce({ issues: rows(100), total: 250 }).mockResolvedValueOnce({ issues: [], total: 250 });
    expect(await runDirectives()).toHaveLength(100);
    expect(listIssues).toHaveBeenCalledTimes(2);
  });
});
