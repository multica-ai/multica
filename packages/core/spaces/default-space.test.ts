import { describe, expect, it } from "vitest";
import type { Space } from "../types";
import { creationDefaultSpaceId, resolveCreationSpaceId } from "./default-space";

function space(overrides: Partial<Space>): Space {
  return {
    id: "space-id",
    workspace_id: "ws-1",
    name: "Space",
    key: "SPC",
    icon: null,
    issue_counter: 0,
    is_default: false,
    visibility: "open",
    archived_at: null,
    created_by: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    is_member: false,
    member_role: null,
    is_pinned: false,
    is_followed: false,
    sort_order: 0,
    ...overrides,
  };
}

describe("creationDefaultSpaceId", () => {
  it("uses the configured workspace default regardless of personal order", () => {
    const spaces = [
      space({ id: "a", is_member: true, sort_order: 2 }),
      space({ id: "b", is_member: true, sort_order: 1 }),
      space({ id: "c", is_default: true, is_member: false, created_at: "2020-01-01T00:00:00Z" }),
    ];
    expect(creationDefaultSpaceId(spaces)).toBe("c");
  });

  it("falls back to the earliest-created active space when the user has no membership rows", () => {
    const spaces = [
      space({ id: "newer", is_member: false, created_at: "2026-02-01T00:00:00Z" }),
      space({ id: "older", is_member: false, created_at: "2026-01-01T00:00:00Z" }),
    ];
    expect(creationDefaultSpaceId(spaces)).toBe("older");
  });

  it("ignores archived spaces in the configured and earliest-created fallbacks", () => {
    const spaces = [
      space({ id: "archived-mine", is_member: true, sort_order: 0, archived_at: "2026-01-01T00:00:00Z" }),
      space({ id: "archived-oldest", created_at: "2020-01-01T00:00:00Z", archived_at: "2026-01-01T00:00:00Z" }),
      space({ id: "active", created_at: "2026-01-02T00:00:00Z" }),
    ];
    expect(creationDefaultSpaceId(spaces)).toBe("active");
  });

  it("returns undefined when there are no active spaces", () => {
    expect(creationDefaultSpaceId([])).toBeUndefined();
  });
});

describe("resolveCreationSpaceId", () => {
  const spaces = [space({ id: "fallback", created_at: "2026-01-01T00:00:00Z" })];

  it("prioritizes structural context over the creation default", () => {
    expect(
      resolveCreationSpaceId(spaces, {
        parentSpaceId: "parent",
        projectSpaceId: "project",
      }),
    ).toBe("parent");
    expect(resolveCreationSpaceId(spaces, { projectSpaceId: "project" })).toBe("project");
  });

  it("falls through to the creation default when no context applies", () => {
    expect(resolveCreationSpaceId(spaces, {})).toBe("fallback");
  });
});
