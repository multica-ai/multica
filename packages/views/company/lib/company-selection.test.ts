// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  companyDepth,
  companySelectionQuery,
  readCompanySelection,
  type CompanySelection,
} from "./company-selection";

const none: CompanySelection = { view: "plan", department: null, squad: null, agent: null };

describe("readCompanySelection", () => {
  it("reads every level from the query string", () => {
    const params = new URLSearchParams("view=overview&dept=f-1&squad=s-1&agent=a-1");
    expect(readCompanySelection(params)).toEqual({
      view: "overview",
      department: "f-1",
      squad: "s-1",
      agent: "a-1",
    });
  });

  it("opens the floor plan for an empty or unknown view", () => {
    expect(readCompanySelection(new URLSearchParams("view=company"))).toEqual(none);
    expect(readCompanySelection(new URLSearchParams("dept="))).toEqual(none);
  });
});

describe("companySelectionQuery", () => {
  it("writes nothing for the default floor plan", () => {
    expect(companySelectionQuery(none)).toBe("");
  });

  it("round-trips a selection", () => {
    const selection: CompanySelection = { view: "overview", department: "unseated", squad: "s-1", agent: "a-2" };
    const query = companySelectionQuery(selection);
    expect(readCompanySelection(new URLSearchParams(query.slice(1)))).toEqual(selection);
  });
});

describe("companyDepth", () => {
  it("counts the open levels", () => {
    expect(companyDepth(none)).toBe(0);
    expect(companyDepth({ ...none, department: "f-1" })).toBe(1);
    expect(companyDepth({ ...none, department: "f-1", squad: "s-1" })).toBe(2);
    expect(companyDepth({ ...none, department: "f-1", squad: "s-1", agent: "a-1" })).toBe(3);
  });

  it("does not open an agent without its squad", () => {
    expect(companyDepth({ ...none, department: "f-1", agent: "a-1" })).toBe(1);
  });
});
