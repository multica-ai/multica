import { beforeEach, describe, expect, it, vi } from "vitest";

const mockListPersonaGrants = vi.hoisted(() => vi.fn());
const mockCreatePersonaGrant = vi.hoisted(() => vi.fn());
const mockUpdatePersonaGrant = vi.hoisted(() => vi.fn());
const mockListPersonaGrantAudit = vi.hoisted(() => vi.fn());

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
      listPersonaGrantAudit: mockListPersonaGrantAudit,
    },
  };
});

import {
  createPersonaGrant,
  fetchGrantAudit,
  fetchPersonaGrants,
  fetchSubjectsWithPermissions,
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
  mockListPersonaGrantAudit.mockReset();
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

describe("fetchSubjectsWithPermissions — derived from the grants list", () => {
  const subjectsFilter = { subjectType: null, search: "", limit: 50, offset: 0 };

  it("folds the flat grant list into one row per subject", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "g1",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "agent-mia",
          resource_pattern: "issue/*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
        {
          id: "g2",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "agent-mia",
          resource_pattern: "repo/*",
          capability: "repo.push",
          status: "pending_approval",
          approval_required: true,
          granted_at: "2026-05-20T10:01:00Z",
          updated_at: "2026-05-20T10:01:00Z",
        },
        {
          id: "g3",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "agent-probe",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:02:00Z",
          updated_at: "2026-05-20T10:02:00Z",
        },
      ],
    });

    const page = await fetchSubjectsWithPermissions("ws-1", subjectsFilter);

    expect(page.total).toBe(2);
    expect(page.items).toHaveLength(2);

    const mia = page.items.find((s) => s.id === "agent-mia");
    expect(mia).toBeDefined();
    expect(mia!.type).toBe("agent");
    expect(mia!.permissions).toHaveLength(2);
    expect(mia!.pending_count).toBe(1);

    const probe = page.items.find((s) => s.id === "agent-probe");
    expect(probe!.permissions).toHaveLength(1);
    expect(probe!.pending_count).toBe(0);
  });

  it("filters subjects by the client-side search term", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "g1",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "agent-mia",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
        {
          id: "g2",
          workspace_id: "ws-1",
          subject_type: "member",
          subject_id: "user-bob",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
      ],
    });

    const page = await fetchSubjectsWithPermissions("ws-1", {
      ...subjectsFilter,
      search: "bob",
    });

    expect(page.items).toHaveLength(1);
    expect(page.items[0]!.id).toBe("user-bob");
  });
});

describe("subject name resolution — backend subject_display_name", () => {
  const subjectsFilter = { subjectType: null, search: "", limit: 50, offset: 0 };

  it("uses the resolved subject_display_name instead of the raw id", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "g1",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "2ed5cfea-3a0e-42c7-94db-77b8a6a276e7",
          subject_display_name: "Mia - CEO EA",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
      ],
    });

    const page = await fetchPersonaGrants("ws-1", filter);

    expect(page.items[0]!.subject.display_name).toBe("Mia - CEO EA");
    expect(page.items[0]!.subject.id).toBe(
      "2ed5cfea-3a0e-42c7-94db-77b8a6a276e7",
    );
  });

  it("falls back to the raw id when the name is missing or empty", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "g1",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "agent-no-name",
          subject_display_name: "",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
      ],
    });

    const page = await fetchPersonaGrants("ws-1", filter);

    expect(page.items[0]!.subject.display_name).toBe("agent-no-name");
  });

  it("surfaces the resolved name on the Subjects tab rows", async () => {
    mockListPersonaGrants.mockResolvedValueOnce({
      grants: [
        {
          id: "g1",
          workspace_id: "ws-1",
          subject_type: "agent",
          subject_id: "2ed5cfea-3a0e-42c7-94db-77b8a6a276e7",
          subject_display_name: "Mia - CEO EA",
          resource_pattern: "*",
          capability: "issues.read",
          status: "active",
          approval_required: false,
          granted_at: "2026-05-20T10:00:00Z",
          updated_at: "2026-05-20T10:00:00Z",
        },
      ],
    });

    const page = await fetchSubjectsWithPermissions("ws-1", subjectsFilter);

    expect(page.items[0]!.display_name).toBe("Mia - CEO EA");
  });
});

describe("audit filter — capability + until forwarded to core client", () => {
  it("passes capability and until to listPersonaGrantAudit", async () => {
    mockListPersonaGrantAudit.mockResolvedValueOnce({ items: [], total: 0, limit: 50, offset: 0 });

    await fetchGrantAudit("ws-1", {
      subjectId: null,
      grantId: null,
      capability: "issues.write",
      since: "2026-01-01T00:00:00Z",
      until: "2026-06-01T00:00:00Z",
      limit: 20,
      offset: 0,
    });

    expect(mockListPersonaGrantAudit).toHaveBeenCalledWith(
      "ws-1",
      expect.objectContaining({
        capability: "issues.write",
        until: "2026-06-01T00:00:00Z",
      }),
    );
  });

  it("omits capability and until when not set", async () => {
    mockListPersonaGrantAudit.mockResolvedValueOnce({ items: [], total: 0, limit: 50, offset: 0 });

    await fetchGrantAudit("ws-1", {
      subjectId: null,
      grantId: null,
      capability: null,
      since: null,
      until: null,
      limit: 50,
      offset: 0,
    });

    expect(mockListPersonaGrantAudit).toHaveBeenCalledOnce();
    const [, filter] = mockListPersonaGrantAudit.mock.calls[0]!;
    expect((filter as Record<string, unknown>).capability).toBeNull();
    expect((filter as Record<string, unknown>).until).toBeNull();
  });
});
