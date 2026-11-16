// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import {
  myIssuesRelationFromScope,
  myIssuesScopeFor,
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

describe("myIssuesScopeFor", () => {
  it("opens My agents' work only from the URL", () => {
    expect(myIssuesScopeFor(new URLSearchParams("scope=agents"), "created")).toBe("agents");
    expect(myIssuesScopeFor(new URLSearchParams(), "created")).toBe("created");
  });

  it("reads a stored agents scope as the My issues default", () => {
    expect(myIssuesScopeFor(new URLSearchParams(), "agents")).toBe("assigned");
  });
});
