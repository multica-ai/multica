import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---------- Hoisted mocks ----------

const mockListGrants = vi.hoisted(() => vi.fn());
const mockCreateGrant = vi.hoisted(() => vi.fn());
const mockUpdateGrant = vi.hoisted(() => vi.fn());
const mockDeleteGrant = vi.hoisted(() => vi.fn());
const mockListRoles = vi.hoisted(() => vi.fn());
const mockCreateRole = vi.hoisted(() => vi.fn());
const mockListRoleAssignments = vi.hoisted(() => vi.fn().mockResolvedValue([]));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listPersonaGrants: mockListGrants,
      getPersonaGrant: vi.fn(),
      createPersonaGrant: mockCreateGrant,
      updatePersonaGrant: mockUpdateGrant,
      deletePersonaGrant: mockDeleteGrant,
      listPersonaGrantAudit: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 }),
      listCerebroRoles: mockListRoles,
      createCerebroRole: mockCreateRole,
      listCerebroRoleAssignments: mockListRoleAssignments,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", name: "Acme" }),
}));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ userId: "user-1", role: "admin", member: null, isLoading: false }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// FIR-2230 phase 7: Roles + Matrix now live on the merged AccessPage (the old
// PermissionsPage was folded in). The tabs are reached the same way.
import { AccessPage } from "./access-page";

function makePage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <AccessPage />
    </QueryClientProvider>
  );
}

const sampleRole = {
  id: "role-1",
  workspace_id: "ws-1",
  name: "Release manager",
  description: "Can ship",
  created_by: null,
  created_at: "2026-05-25T10:00:00Z",
  updated_at: "2026-05-25T10:00:00Z",
};

beforeEach(() => {
  mockListGrants.mockReset().mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 });
  mockCreateGrant.mockReset().mockResolvedValue(null);
  mockUpdateGrant.mockReset().mockResolvedValue(null);
  mockDeleteGrant.mockReset().mockResolvedValue(undefined);
  mockListRoles.mockReset().mockResolvedValue([sampleRole]);
  mockCreateRole.mockReset().mockResolvedValue(sampleRole);
  mockListRoleAssignments.mockReset().mockResolvedValue([]);
});

describe("Roles tab", () => {
  it("lists roles from the API", async () => {
    const user = userEvent.setup();
    render(makePage());
    await user.click(screen.getByRole("tab", { name: "Roles" }));
    expect(await screen.findByText("Release manager")).toBeInTheDocument();
    expect(mockListRoles).toHaveBeenCalledWith("ws-1");
  }, 15_000);
});

describe("Authoring matrix tab", () => {
  it("renders a subject row per role plus the workspace default, and capability columns", async () => {
    const user = userEvent.setup();
    render(makePage());
    await user.click(screen.getByRole("tab", { name: "Matrix" }));

    expect(await screen.findByText(/Everyone \(workspace default\)/i)).toBeInTheDocument();
    expect(screen.getByText("Release manager")).toBeInTheDocument();
    // A capability column from the catalog.
    expect(screen.getByText("shell.exec")).toBeInTheDocument();
  }, 15_000);

  it("creates a workspace-wide grant when an empty cell is clicked", async () => {
    const user = userEvent.setup();
    render(makePage());
    await user.click(screen.getByRole("tab", { name: "Matrix" }));

    const cell = await screen.findByRole("button", {
      name: /Everyone \(workspace default\) · Run a shell command: none/i,
    });
    await user.click(cell);

    await waitFor(() => expect(mockCreateGrant).toHaveBeenCalledTimes(1));
    const body = mockCreateGrant.mock.calls[0]![1];
    expect(body.subject_type).toBe("workspace_default");
    expect(body.capability).toBe("shell.exec");
    expect(body.resource_pattern).toBe("*");
    expect(body.approval_required).toBe(false);
  }, 15_000);
});
