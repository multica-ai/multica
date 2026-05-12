import { describe, it, expect } from "vitest";
import {
  cerebroGroupSchema,
  cerebroGroupListSchema,
  cerebroGroupMemberSchema,
  cerebroGroupMemberListSchema,
} from "./types";

describe("cerebroGroupSchema", () => {
  it("parses a fully populated row", () => {
    const result = cerebroGroupSchema.safeParse({
      id: "g-1",
      workspace_id: "ws-1",
      name: "Eng",
      description: "desc",
      created_by: "u-1",
      created_at: "2026-05-11T00:00:00Z",
      updated_at: "2026-05-11T00:00:00Z",
    });
    expect(result.success).toBe(true);
  });

  it("accepts null description and created_by", () => {
    const result = cerebroGroupSchema.safeParse({
      id: "g-1",
      workspace_id: "ws-1",
      name: "Eng",
      description: null,
      created_by: null,
      created_at: "2026-05-11T00:00:00Z",
      updated_at: "2026-05-11T00:00:00Z",
    });
    expect(result.success).toBe(true);
  });

  it("rejects payloads missing required fields", () => {
    const result = cerebroGroupSchema.safeParse({
      // missing id + workspace_id
      name: "Eng",
      created_at: "",
      updated_at: "",
    });
    expect(result.success).toBe(false);
  });
});

describe("cerebroGroupListSchema", () => {
  it("parses an empty array", () => {
    const result = cerebroGroupListSchema.safeParse([]);
    expect(result.success).toBe(true);
  });

  it("rejects a non-array payload", () => {
    const result = cerebroGroupListSchema.safeParse({ groups: [] });
    expect(result.success).toBe(false);
  });
});

describe("cerebroGroupMemberSchema", () => {
  it("parses a member row with all fields", () => {
    const result = cerebroGroupMemberSchema.safeParse({
      group_id: "g-1",
      user_id: "u-1",
      added_by: "u-2",
      added_at: "2026-05-11T00:00:00Z",
      user_name: "Anne",
      user_email: "anne@example.test",
      user_avatar_url: "https://example.test/avatar.png",
    });
    expect(result.success).toBe(true);
  });

  it("accepts null added_by and user_avatar_url", () => {
    const result = cerebroGroupMemberSchema.safeParse({
      group_id: "g-1",
      user_id: "u-1",
      added_by: null,
      added_at: "",
      user_name: "Anne",
      user_email: "anne@example.test",
      user_avatar_url: null,
    });
    expect(result.success).toBe(true);
  });
});

describe("cerebroGroupMemberListSchema", () => {
  it("parses an empty array", () => {
    const result = cerebroGroupMemberListSchema.safeParse([]);
    expect(result.success).toBe(true);
  });
});
