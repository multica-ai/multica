import { describe, expect, it } from "vitest";
import {
  advancedFilterFromSimple,
  advancedFilterSignature,
  advancedFilterToTableQuery,
  countAdvancedConditions,
  createFilterCondition,
  createFilterGroup,
  dueDateMatches,
  editAdvancedFilter,
  EMPTY_SIMPLE_FILTER_FIELDS,
  foldPropertyFiltersIntoTree,
  pruneAdvancedFilter,
  sanitizeAdvancedFilter,
  simpleFromAdvancedFilter,
  statusColumnFilter,
  withoutAdvancedFilterFields,
  type SimpleFilterFields,
} from "./filter-tree";

function simple(patch: Partial<SimpleFilterFields>): SimpleFilterFields {
  return { ...EMPTY_SIMPLE_FILTER_FIELDS, ...patch };
}

describe("dueDateMatches", () => {
  // Wednesday.
  const today = "2026-03-11";

  it.each([
    ["2026-03-10", "overdue", true],
    ["2026-03-11", "overdue", false],
    ["2026-03-11", "today", true],
    ["2026-03-09", "this_week", true],
    ["2026-03-15", "this_week", true],
    ["2026-03-16", "this_week", false],
    ["2026-03-08", "this_week", false],
    ["2026-03-11", "next_7_days", true],
    ["2026-03-17", "next_7_days", true],
    ["2026-03-18", "next_7_days", false],
    ["2026-03-10", "next_7_days", false],
    [null, "none", true],
    ["2026-03-11", "none", false],
    [null, "overdue", false],
  ] as const)("%s in %s → %s", (due, bucket, expected) => {
    expect(dueDateMatches(due, bucket, today)).toBe(expected);
  });

  it("starts the week on Monday when today is Sunday", () => {
    expect(dueDateMatches("2026-03-09", "this_week", "2026-03-15")).toBe(true);
    expect(dueDateMatches("2026-03-16", "this_week", "2026-03-15")).toBe(false);
  });
});

describe("simple ↔ advanced", () => {
  it("turns each dimension into one condition and back", () => {
    const fields = simple({
      statusFilters: ["todo", "in_progress"],
      assigneeFilters: [{ type: "agent", id: "a1" }],
      includeNoAssignee: true,
      projectFilters: ["p1"],
      dueDateFilters: ["overdue"],
      negatedFilters: ["status"],
    });
    const tree = advancedFilterFromSimple(fields, fields.negatedFilters);
    expect(tree.match).toBe("all");
    expect(tree.children.map((c) => c.type === "condition" && [c.field, c.negated, c.values])).toEqual([
      ["status", true, ["todo", "in_progress"]],
      ["assignee", false, ["agent:a1", "none"]],
      ["project", false, ["p1"]],
      ["dueDate", false, ["overdue"]],
    ]);
    expect(simpleFromAdvancedFilter(tree)).toEqual(fields);
  });

  it("refuses what simple mode cannot say", () => {
    const status = createFilterCondition("status", ["todo"]);
    const label = createFilterCondition("label", ["l1"]);
    expect(simpleFromAdvancedFilter(createFilterGroup("any", [status, label]))).toBeNull();
    expect(
      simpleFromAdvancedFilter(createFilterGroup("all", [status, createFilterCondition("status", ["done"], true)])),
    ).toBeNull();
    expect(simpleFromAdvancedFilter(createFilterGroup("all", [createFilterGroup("any", [status])]))).toBeNull();
  });

  it("carries property filters as conditions", () => {
    const fields = simple({
      priorityFilters: ["urgent"],
      propertyFilters: { p1: ["opt-a", "__none__"], p2: [{ op: "gte", value: "3" }] },
    });
    const tree = advancedFilterFromSimple(fields, []);
    expect(tree.children.map((c) => c.type === "condition" && [c.field, c.values])).toEqual([
      ["priority", ["urgent"]],
      ["property:p1", ["opt-a", "__none__"]],
      ["property:p2", [{ op: "gte", value: "3" }]],
    ]);
    expect(simpleFromAdvancedFilter(tree)).toEqual(fields);
    // Simple mode has no "is not" for a property.
    expect(
      simpleFromAdvancedFilter(createFilterGroup("all", [createFilterCondition("property:p1", ["opt-a"], true)])),
    ).toBeNull();
  });

  it("accepts a lone condition under any, and ignores empty rows", () => {
    const status = createFilterCondition("status", ["todo"]);
    const tree = createFilterGroup("any", [status, createFilterCondition("label")]);
    expect(simpleFromAdvancedFilter(tree)?.statusFilters).toEqual(["todo"]);
  });
});

describe("tree helpers", () => {
  it("prunes empty conditions and groups", () => {
    const tree = createFilterGroup("all", [
      createFilterCondition("status"),
      createFilterGroup("any", [createFilterCondition("label")]),
    ]);
    expect(pruneAdvancedFilter(tree)).toBeNull();
    expect(countAdvancedConditions(tree)).toBe(0);
  });

  it("edits, removes and duplicates by id", () => {
    const a = createFilterCondition("status", ["todo"]);
    const b = createFilterCondition("label", ["l1"]);
    const group = createFilterGroup("any", [b]);
    const root = createFilterGroup("all", [a, group]);
    const removed = editAdvancedFilter(root, b.id, () => []);
    expect((removed.children[1] as typeof group).children).toEqual([]);
    const duplicated = editAdvancedFilter(root, a.id, (node) => [node, { ...node, id: "copy" }]);
    expect(duplicated.children.map((c) => c.id)).toEqual([a.id, "copy", group.id]);
    // The input is never mutated.
    expect(root.children).toHaveLength(2);
  });

  it("compares trees without their ids", () => {
    const one = createFilterGroup("all", [createFilterCondition("status", ["b", "a"])]);
    const two = createFilterGroup("all", [createFilterCondition("status", ["a", "b"])]);
    expect(advancedFilterSignature(one)).toBe(advancedFilterSignature(two));
  });

  it("strips the fields a scope fixes", () => {
    const tree = createFilterGroup("all", [
      createFilterCondition("relation", ["assigned"]),
      createFilterGroup("any", [createFilterCondition("project", ["p1"])]),
    ]);
    expect(withoutAdvancedFilterFields(tree, ["relation", "project"])).toBeNull();
    expect(countAdvancedConditions(withoutAdvancedFilterFields(tree, ["project"]))).toBe(1);
  });

  it("folds flat property filters into a tree, ANDed like they were", () => {
    const status = createFilterCondition("status", ["todo"]);
    const label = createFilterCondition("label", ["l1"]);
    const props = { p1: ["opt-a"] };
    const all = foldPropertyFiltersIntoTree(createFilterGroup("all", [status]), props);
    expect(all?.children.map((c) => c.type === "condition" && c.field)).toEqual(["status", "property:p1"]);

    const any = foldPropertyFiltersIntoTree(createFilterGroup("any", [status, label]), props);
    expect(any?.match).toBe("all");
    expect(any?.children[0]).toMatchObject({ type: "group", match: "any", children: [status, label] });
    expect(any?.children[1]).toMatchObject({ field: "property:p1", values: ["opt-a"] });

    // An "any" root that already holds a group can't take another level.
    expect(
      foldPropertyFiltersIntoTree(createFilterGroup("any", [status, createFilterGroup("all", [label])]), props),
    ).toBeNull();
    const untouched = createFilterGroup("any", [status]);
    expect(foldPropertyFiltersIntoTree(untouched, { p1: [] })).toBe(untouched);
  });

  it("converts to the table query", () => {
    const tree = createFilterGroup("all", [
      createFilterCondition("project", ["p1"]),
      createFilterGroup("any", [
        createFilterCondition("priority", ["urgent"]),
        createFilterCondition("dueDate", ["overdue"], true),
        createFilterCondition("label"),
        createFilterCondition("property:p1", [{ op: "lt", value: "3" }, "__none__"], true),
      ]),
    ]);
    expect(advancedFilterToTableQuery(tree)).toEqual({
      match: "all",
      children: [
        { field: "project_ids", values: ["p1"] },
        {
          match: "any",
          children: [
            { field: "priorities", values: ["urgent"] },
            { field: "due_dates", negate: true, values: ["overdue"] },
            { field: "property:p1", negate: true, values: [{ op: "lt", value: "3" }, "__none__"] },
          ],
        },
      ],
    });
  });
});

describe("sanitizeAdvancedFilter", () => {
  it("keeps known fields and values and drops the rest", () => {
    const tree = sanitizeAdvancedFilter({
      type: "group",
      id: "root",
      match: "any",
      children: [
        { type: "condition", id: "c1", field: "priority", negated: true, values: ["urgent", "bogus", "urgent"] },
        { type: "condition", id: "c2", field: "nope", values: ["x"] },
        { type: "condition", id: "c3", field: "creator", values: ["none", "member:u1", "robot:x"] },
        {
          type: "condition",
          id: "c4",
          field: "property:p1",
          values: ["opt-a", "", 3, { op: "gte", value: "2" }, { op: "bogus", value: "2" }, "opt-a"],
        },
        { type: "condition", id: "c5", field: "property:", values: ["x"] },
        {
          type: "group",
          id: "g1",
          match: "all",
          // Deeper than one level is not representable.
          children: [{ type: "group", id: "g2", match: "all", children: [] }],
        },
      ],
    });
    expect(tree).toEqual({
      type: "group",
      id: "root",
      match: "any",
      children: [
        { type: "condition", id: "c1", field: "priority", negated: true, values: ["urgent"] },
        { type: "condition", id: "c3", field: "creator", negated: false, values: ["member:u1"] },
        {
          type: "condition",
          id: "c4",
          field: "property:p1",
          negated: false,
          values: ["opt-a", { op: "gte", value: "2" }],
        },
        { type: "group", id: "g1", match: "all", children: [] },
      ],
    });
  });

  it("rejects anything that is not a group", () => {
    expect(sanitizeAdvancedFilter(null)).toBeNull();
    expect(sanitizeAdvancedFilter({ type: "condition", field: "status", values: ["x"] })).toBeNull();
  });
});

describe("statusColumnFilter", () => {
  const hidden = ["cancelled", "done"];

  it("keeps the simple status filter, which the tree is ANDed with", () => {
    const tree = createFilterGroup("all", [createFilterCondition("status", ["todo"])]);
    expect(statusColumnFilter(["cancelled"], false, tree, hidden)).toEqual({
      statuses: ["cancelled"],
      negated: false,
      hiddenStatuses: hidden,
    });
  });

  it("picks columns from a status every row must meet", () => {
    const tree = createFilterGroup("all", [
      createFilterCondition("status", ["cancelled"], true),
      createFilterGroup("any", [createFilterCondition("priority", ["urgent"])]),
    ]);
    expect(statusColumnFilter([], false, tree, hidden)).toEqual({
      statuses: ["cancelled"],
      negated: true,
      hiddenStatuses: hidden,
    });
  });

  it("un-hides a status a looser tree names, without narrowing", () => {
    const tree = createFilterGroup("any", [
      createFilterCondition("status", ["cancelled"]),
      createFilterCondition("status", ["done"], true),
      createFilterCondition("priority", ["urgent"]),
    ]);
    expect(statusColumnFilter([], false, tree, hidden)).toEqual({
      statuses: [],
      negated: false,
      hiddenStatuses: ["done"],
    });
  });
});
