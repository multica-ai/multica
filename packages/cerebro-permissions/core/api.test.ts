import { beforeEach, describe, expect, it, vi } from "vitest";

const mockListPersonaGrants = vi.hoisted(() => vi.fn());
const mockCreatePersonaGrant = vi.hoisted(() => vi.fn());
const mockUpdatePersonaGrant = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listPersonaGrants: mockListPersonaGrants,
      createPersonaGrant: mockCreatePersonaGrant,
      updatePersonaGrant: mockUpdatePersonaGrant,
    },
  };
});

import {
  createPersonaGrant,
  fetchPersonaGrants,
  updatePersonaGrant,
} from "./api";
import type { CreatePersonaGrantRequest, GrantsFilter } from "./types";

const filter: GrantsFilter = {
  subjectType: null,
  subjectId: null,
  resourceType: null,
  status: null,
  classification: null,
  search: "",
  limit: 50,
  offset: 0,
};

beforeEach(() => {
  mockListPersonaGrants.mockReset();
  mockCreatePersonaGrant.mockReset();
  mockUpdatePersonaGrant.mockReset();
});

describe("permissions api compatibility", () => {
  it("normalizes the flat backend grant list into the Persona UI shape", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "grant-1",
          workspace_id: "ws-1",
          subject_type: "member",
          subject_id: "user-1",
          resource_pattern: "issue/*",
          capability: "issues.read",
          classification_ceiling: "internal",
          approval_required: true,
          status: "active",
          granted_by_id: "admin-1",
          granted_at: "2026-05-16T10:00:00Z",
          updated_at: "2026-05-16T10:01:00Z",
        },
      ],
    });

    const page = await fetchPersonaGrants("ws-1", filter);

    expect(page.items[0]).toMatchObject({
      subject: { type: "member", id: "user-1" },
      resource: { type: "issue", pattern: "issue/*" },
      capability: "issues.read",
      approval_required: true,
    });
  });

  it("maps create and revoke requests to the flat backend contract", async () => {
    const body: CreatePersonaGrantRequest = {
      subject: { type: "member", id: "user-1", display_name: null },
      resource: { type: "issue", pattern: "issue/*" },
      capability: "issues.read",
      classification_ceiling: "internal",
      approval_required: false,
      time_window: null,
    };
    mockCreatePersonaGrant.mockResolvedValueOnce({
      id: "grant-1",
      workspace_id: "ws-1",
      subject_type: "member",
      subject_id: "user-1",
      resource_pattern: "issue/*",
      capability: "issues.read",
      status: "active",
      approval_required: false,
      granted_at: "2026-05-16T10:00:00Z",
      updated_at: "2026-05-16T10:00:00Z",
    });
    mockUpdatePersonaGrant.mockResolvedValueOnce({
      id: "grant-1",
      workspace_id: "ws-1",
      subject_type: "member",
      subject_id: "user-1",
      resource_pattern: "issue/*",
      capability: "issues.read",
      status: "revoked",
      approval_required: false,
      granted_at: "2026-05-16T10:00:00Z",
      updated_at: "2026-05-16T10:02:00Z",
    });

    await createPersonaGrant("ws-1", body);
    await updatePersonaGrant("ws-1", "grant-1", { status: "revoked" });

    expect(mockCreatePersonaGrant).toHaveBeenCalledWith("ws-1", {
      subject_type: "member",
      subject_id: "user-1",
      resource_pattern: "issue/*",
      capability: "issues.read",
      classification_ceiling: "internal",
      time_window_start: null,
      time_window_end: null,
      approval_required: false,
    });
    expect(mockUpdatePersonaGrant).toHaveBeenCalledWith("ws-1", "grant-1", {
      resource_pattern: undefined,
      capability: undefined,
      classification_ceiling: undefined,
      time_window_start: null,
      time_window_end: null,
      approval_required: undefined,
      status: "revoked",
    });
  });
});
