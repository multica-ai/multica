import { describe, expect, it } from "vitest";
import { isProjectArchived, selectableProjects } from "./archive";

describe("selectableProjects", () => {
  const live = { id: "live", archived_at: null };
  const archived = { id: "archived", archived_at: "2026-01-01T00:00:00Z" };

  it("drops archived projects", () => {
    expect(selectableProjects([live, archived])).toEqual([live]);
    expect(isProjectArchived(archived)).toBe(true);
    expect(isProjectArchived(live)).toBe(false);
  });

  it("keeps the archived project that is already selected", () => {
    expect(selectableProjects([live, archived], "archived")).toEqual([live, archived]);
  });
});
