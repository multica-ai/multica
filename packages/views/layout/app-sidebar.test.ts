import { describe, expect, it } from "vitest";

import { getCreateIssueModalData } from "./app-sidebar";

describe("getCreateIssueModalData", () => {
  it("prefills the project on a project detail route", () => {
    expect(getCreateIssueModalData("/acme/projects/project-123")).toEqual({
      project_id: "project-123",
    });
  });

  it("does not prefill outside a project detail route", () => {
    expect(getCreateIssueModalData("/acme/issues")).toBeUndefined();
    expect(getCreateIssueModalData("/acme/projects")).toBeUndefined();
  });
});
