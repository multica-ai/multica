import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import {
  getIssueGroupId,
  getMoveUpdates,
  issueMatchesGroup,
  propertyGroupId,
} from "./drag-utils";

describe("property grouping", () => {
  const propertyId = "prop-env";
  const withValue = {
    id: "A",
    properties: { [propertyId]: "opt-staging" },
  } as unknown as Issue;
  const withoutValue = { id: "B", properties: {} } as unknown as Issue;

  it("buckets issues by option and keeps empty values in the no-value column", () => {
    expect(getIssueGroupId(withValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-staging"),
    );
    expect(getIssueGroupId(withoutValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, null),
    );
  });

  it("matches option and no-value columns independently", () => {
    const optionColumn = {
      id: "c1",
      title: "Staging",
      propertyId,
      propertyOptionId: "opt-staging",
    };
    const noneColumn = {
      id: "c2",
      title: "No value",
      propertyId,
      propertyOptionId: null,
    };

    expect(issueMatchesGroup(withValue, optionColumn)).toBe(true);
    expect(issueMatchesGroup(withValue, noneColumn)).toBe(false);
    expect(issueMatchesGroup(withoutValue, noneColumn)).toBe(true);
  });

  it("moves unknown option values into no-value when the catalog is known", () => {
    const stale = {
      id: "C",
      properties: { [propertyId]: "opt-deleted" },
    } as unknown as Issue;
    const known = new Set(["opt-staging"]);

    expect(getIssueGroupId(stale, `property:${propertyId}`, known)).toBe(
      propertyGroupId(propertyId, null),
    );
  });

  it("keeps property writes out of the normal issue update payload", () => {
    expect(
      getMoveUpdates(
        {
          id: "c1",
          title: "Staging",
          propertyId,
          propertyOptionId: "opt-staging",
        },
        5,
      ),
    ).toEqual({ position: 5 });
  });
});
