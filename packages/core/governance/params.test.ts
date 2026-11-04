// @vitest-environment node
import { describe, expect, it } from "vitest";
import { governanceEventSearch, parseGovernanceEventFilters } from "./params";

describe("governanceEventSearch", () => {
  it("leaves out empty filters so the server reads them as all", () => {
    expect(governanceEventSearch({}).toString()).toBe("");
  });

  it("writes the filters the log can be narrowed by", () => {
    const search = governanceEventSearch({ kind: "budget_hold", actor_type: "agent", subject_type: "squad" });
    expect(search.get("kind")).toBe("budget_hold");
    expect(search.get("actor_type")).toBe("agent");
    expect(search.get("subject_type")).toBe("squad");
  });

  it("adds the cursor and limit only when given", () => {
    expect(governanceEventSearch({}, { cursor: "c-9", limit: 50 }).toString()).toBe("cursor=c-9&limit=50");
    expect(governanceEventSearch({}, { cursor: null }).toString()).toBe("");
  });
});

describe("parseGovernanceEventFilters", () => {
  it("reads the filters the page URL carries", () => {
    const filters = parseGovernanceEventFilters(
      new URLSearchParams("tab=log&kind=budget_hold&actor_type=agent&subject_type=squad"),
    );
    expect(filters).toEqual({ kind: "budget_hold", actor_type: "agent", subject_type: "squad" });
  });

  it("ignores values the client does not offer, and empty ones", () => {
    const filters = parseGovernanceEventFilters(
      new URLSearchParams("kind=made_up&actor_type=robot&subject_type=&tab=log"),
    );
    expect(filters).toEqual({});
  });
});
