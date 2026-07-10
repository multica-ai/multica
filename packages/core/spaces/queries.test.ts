import { describe, expect, it } from "vitest";
import type { ListSpacesResponse, Space } from "../types";
import { mySpaceListOptions } from "./queries";

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

describe("mySpaceListOptions", () => {
  it("shows joined or pinned active Spaces in personal order", () => {
    const data: ListSpacesResponse = {
      spaces: [
        space({ id: "joined", is_member: true, sort_order: 20 }),
        space({ id: "pinned", is_pinned: true, sort_order: 10 }),
        space({ id: "followed", is_followed: true, sort_order: 5 }),
        space({ id: "discoverable" }),
        space({ id: "archived", is_pinned: true, archived_at: "2026-02-01T00:00:00Z" }),
      ],
      total: 5,
    };

    const select = mySpaceListOptions("ws-1").select;
    expect(select?.(data).map((item) => item.id)).toEqual(["pinned", "joined"]);
  });
});
