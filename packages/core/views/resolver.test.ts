import { describe, it, expect } from "vitest";
import { resolveViewRequests, dedupeIssuesById } from "./resolver";
import type { Issue } from "../types";

describe("resolveViewRequests", () => {
  it("returns a single request for a flat filter set", () => {
    const reqs = resolveViewRequests({ priorities: ["high"], statuses: ["todo"] });
    expect(reqs).toEqual([{ priorities: ["high"], statuses: ["todo"] }]);
  });

  it("returns a single request when any_of is empty", () => {
    const reqs = resolveViewRequests({ any_of: [], priorities: ["high"] });
    expect(reqs).toEqual([{ priorities: ["high"] }]);
  });

  it("fans out any_of into one request per branch, ANDing the outer filters", () => {
    const reqs = resolveViewRequests({
      any_of: [
        { assignee_filters: ["member:{me}"] },
        { creator_filters: ["member:{me}"] },
        { assignee_filters: ["{my_agents}", "{my_squads}"] },
      ],
      priorities: ["high"],
    });
    expect(reqs).toEqual([
      { assignee_filters: ["member:{me}"], priorities: ["high"] },
      { creator_filters: ["member:{me}"], priorities: ["high"] },
      { assignee_filters: ["{my_agents}", "{my_squads}"], priorities: ["high"] },
    ]);
  });

  it("never leaks the any_of key into a request", () => {
    for (const req of resolveViewRequests({ any_of: [{ statuses: ["done"] }] })) {
      expect("any_of" in req).toBe(false);
    }
  });
});

describe("dedupeIssuesById", () => {
  const mk = (id: string): Issue => ({ id }) as Issue;

  it("merges arrays keeping the first occurrence and preserving order", () => {
    const a = [mk("1"), mk("2")];
    const b = [mk("2"), mk("3")];
    expect(dedupeIssuesById([a, b]).map((i) => i.id)).toEqual(["1", "2", "3"]);
  });

  it("returns an empty array for no inputs", () => {
    expect(dedupeIssuesById([])).toEqual([]);
  });
});
