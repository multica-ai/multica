// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue, IssueProperty } from "../types";
import { copyableParentProperties, copyParentIssueFields, subIssueCreateDefaults } from "./sub-issue-create";

const parent = {
  id: "parent", identifier: "TEST-1", project_id: "project",
  assignee_type: "member", assignee_id: "owner", status: "in_progress",
  priority: "high", description: "Parent scope", start_date: "2026-11-05", due_date: "2026-11-20",
  labels: [{ id: "label", name: "Product" }],
  properties: { customer: "Acme", budget: 100000, targets: ["A", "B"], approved: false, zero: 0, archived: "old", future: "unknown" },
} as unknown as Issue;
const property = (id: string, type = "text", archived = false) => ({ id, name: id, type, archived }) as IssueProperty;
const catalog = [property("customer"), property("budget", "number"), property("targets", "multi_text"), property("approved", "checkbox"), property("zero", "number"), property("archived", "text", true), property("future", "future")];

describe("sub-issue create context", () => {
  it.each(["member", "agent", "squad"] as const)("seeds project and %s assignee, never unrelated fields", (type) => {
    expect(subIssueCreateDefaults({ ...parent, assignee_type: type })).toEqual({
      parent_issue_id: "parent", parent_issue_identifier: "TEST-1", project_id: "project", assignee_type: type, assignee_id: "owner",
    });
  });
  it("omits absent or incomplete assignees rather than clearing sticky defaults", () => {
    expect(subIssueCreateDefaults({ id: "parent", assignee_type: "member", assignee_id: null })).toEqual({ parent_issue_id: "parent", parent_issue_identifier: undefined });
  });
});

describe("explicit parent field copies", () => {
  it("copies nothing by default", () => expect(copyParentIssueFields(parent, [], catalog)).toEqual({}));
  it("copies only selected fields and custom properties", () => {
    expect(copyParentIssueFields(parent, ["priority", "labels", "property:customer"], catalog)).toEqual({ priority: "high", label_ids: ["label"], properties: { customer: "Acme" } });
  });
  it("supports dates, false and zero without copying description, status or assignee", () => {
    expect(copyParentIssueFields(parent, ["start_date", "due_date", "property:approved", "property:zero"], catalog)).toEqual({ start_date: "2026-11-05", due_date: "2026-11-20", properties: { approved: false, zero: 0 } });
  });
  it("does not offer archived, unavailable or unknown property definitions", () => {
    expect(copyableParentProperties(parent, catalog).map((p) => p.id)).toEqual(["customer", "budget", "targets", "approved", "zero"]);
    expect(copyParentIssueFields(parent, ["property:archived", "property:future", "property:deleted"], catalog)).toEqual({});
  });
  it("excludes single-actor values this client cannot edit", () => {
    const source = { ...parent, properties: { external: "agent:remote" } };
    expect(copyParentIssueFields(source, ["property:external"], [property("external", "actor")])).toEqual({});
  });
  it("detaches arrays from the parent and retains explicit empty label copies", () => {
    const copy = copyParentIssueFields(parent, ["labels", "property:targets"], catalog);
    expect(copy.properties?.targets).not.toBe(parent.properties.targets);
    copy.label_ids?.push("new");
    expect(parent.labels).toHaveLength(1);
    expect(copyParentIssueFields({ ...parent, labels: [], start_date: null }, ["labels", "start_date"], catalog)).toEqual({ label_ids: [] });
  });
});
