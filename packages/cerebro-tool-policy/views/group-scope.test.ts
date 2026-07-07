import { describe, expect, it } from "vitest";
import { mapCerebroGroupsToScopeOptions } from "./group-scope";

describe("mapCerebroGroupsToScopeOptions", () => {
  it("maps group API rows to picker options and drops rows without an id", () => {
    const options = mapCerebroGroupsToScopeOptions([
      { id: "g1", name: "Finance" },
      { id: "g2", name: "Customer service" },
      { id: "", name: "Broken" },
    ]);

    expect(options).toEqual([
      { id: "g1", name: "Finance" },
      { id: "g2", name: "Customer service" },
    ]);
  });

  it("falls back to the id when a group has no usable name", () => {
    expect(mapCerebroGroupsToScopeOptions([{ id: "g1", name: "" }])).toEqual([
      { id: "g1", name: "g1" },
    ]);
  });
});
