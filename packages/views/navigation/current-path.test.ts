// @vitest-environment node
import { describe, expect, it } from "vitest";
import { currentPath } from "./current-path";

function location(pathname: string, search: string, hash: string) {
  return { pathname, searchParams: new URLSearchParams(search), hash };
}

describe("currentPath", () => {
  it("returns the pathname alone when there is no search or fragment", () => {
    expect(currentPath(location("/acme/issues", "", ""))).toBe("/acme/issues");
  });

  it("appends the search string", () => {
    expect(currentPath(location("/acme/issues", "view=board", ""))).toBe(
      "/acme/issues?view=board",
    );
  });

  // MUL-6784: a share/feedback link rebuilt without the fragment silently
  // points at the whole issue instead of the comment the user was reading.
  it("keeps the fragment, with and without a search string", () => {
    expect(currentPath(location("/acme/issues/MUL-1", "", "#comment-c1"))).toBe(
      "/acme/issues/MUL-1#comment-c1",
    );
    expect(
      currentPath(location("/acme/issues/MUL-1", "tab=activity", "#comment-c1")),
    ).toBe("/acme/issues/MUL-1?tab=activity#comment-c1");
  });
});
