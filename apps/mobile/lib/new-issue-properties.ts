import type { IssueProperty } from "@multica/core/types";

/**
 * Properties the new-issue form shows: active single-select properties that
 * are required or always visible. Mobile has a picker for single select only,
 * which is also the only type the server lets an admin make required.
 */
export function newIssueFormProperties(properties: readonly IssueProperty[]): IssueProperty[] {
  return properties.filter(
    (p) =>
      !p.archived &&
      p.type === "select" &&
      (p.required === true || p.always_visible === true),
  );
}
