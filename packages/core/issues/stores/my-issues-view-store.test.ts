// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import {
  myIssuesRelationFromScope,
  type MyIssuesScope,
} from "./my-issues-view-store";

describe("myIssuesRelationFromScope", () => {
  it.each([
    ["all", "all"],
    ["assigned", "assigned"],
    ["created", "created"],
    ["agents", "involved"],
  ] satisfies Array<[MyIssuesScope, string]>)(
    "maps %s to the surface relation %s",
    (scope, relation) => {
      expect(myIssuesRelationFromScope(scope)).toBe(relation);
    },
  );
});
