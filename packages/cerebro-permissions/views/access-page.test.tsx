import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---------- Hoisted mocks ----------

const mockListGrants = vi.hoisted(() => vi.fn());
const mockGetGrant = vi.hoisted(() => vi.fn());
const mockCreateGrant = vi.hoisted(() => vi.fn());
const mockUpdateGrant = vi.hoisted(() => vi.fn());
const mockDeleteGrant = vi.hoisted(() => vi.fn());
const mockAudit = vi.hoisted(() => vi.fn().mockResolvedValue({
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listPersonaGrants: mockListGrants,
      getPersonaGrant: mockGetGrant,
      createPersonaGrant: mockCreateGrant,
      updatePersonaGrant: mockUpdateGrant,
      deletePersonaGrant: mockDeleteGrant,
      listPersonaGrantAudit: mockAudit,
      listSubjectsWithPermissions: undefined,
      listPendingApprovalAsks: undefined,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", name: "Acme" }),
}));

type MockCurrentMember = {
  userId: string | null;
  role: "owner" | "admin" | "member" | null;
  member: null;
  isLoading: boolean;
};
const mockUseCurrentMember = vi.hoisted(() =>
  vi.fn<() => MockCurrentMember>(() => ({
    userId: "user-1",
    role: "admin",
    member: null,
    isLoading: false,
  })),
);
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: mockUseCurrentMember,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { AccessPage } from "./access-page";

function makePage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    qc,
    ui: (
      <QueryClientProvider client={qc}>
        <AccessPage />
      </QueryClientProvider>
    ),
  };
}

const sampleGrant = {
  id: "grant-1",
  workspace_id: "ws-1",
  subject: { type: "agent", id: "agt-1", display_name: "Fætta", avatar_url: null },
  resource: { type: "issue", pattern: "*" },
  capability: "issue.read",
  classification_ceiling: "internal",
  status: "active",
  approval_required: false,
  time_window: null,
  description: null,
  created_by: null,
  created_at: "2026-05-24T10:00:00Z",
  updated_at: "2026-05-24T10:00:00Z",
};

beforeEach(() => {
  mockListGrants.mockReset();
  mockGetGrant.mockReset();
  mockCreateGrant.mockReset();
  mockUpdateGrant.mockReset();
  mockDeleteGrant.mockReset();
  mockAudit.mockReset();
  mockAudit.mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 });
  mockUseCurrentMember.mockReturnValue({
    userId: "user-1",
    role: "admin" as const,
    member: null,
    isLoading: false,
  });
});

describe("AccessPage", () => {
  it("renders Subjects tab as default active tab", async () => {
    const { ui } = makePage();
    render(ui);
    // Subjects tab trigger should be visible
    expect(await screen.findByRole("tab", { name: /Subjects/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Pending/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Grants/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Audit/i })).toBeInTheDocument();
  }, 15_000);

  it("shows access denied for non-admin members", async () => {
    // Use mockReturnValue (not Once) so all re-renders from React Query refetch also see "member"
    mockUseCurrentMember.mockReturnValue({
      userId: "user-2",
      role: "member" as const,
      member: null,
      isLoading: false,
    });
    const { ui } = makePage();
    render(ui);
    expect(await screen.findByText(/Adgang nægtet/i)).toBeInTheDocument();
  }, 15_000);

  it("Pending tab shows empty state when no pending asks", async () => {
    const user = userEvent.setup();
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Pending/i }));
    expect(await screen.findByText(/Ingen afventende godkendelser/i)).toBeInTheDocument();
  }, 15_000);

  it("Grants tab renders table when switched to", async () => {
    const user = userEvent.setup();
    mockListGrants.mockResolvedValue({
      items: [sampleGrant],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Grants/i }));
    expect(await screen.findByText("Fætta")).toBeInTheDocument();
    expect(screen.getByText("issue.read")).toBeInTheDocument();
  }, 15_000);

  it("Audit tab renders empty state when no audit events", async () => {
    const user = userEvent.setup();
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    expect(await screen.findByText(/Ingen audit-hændelser/i)).toBeInTheDocument();
  }, 15_000);

  it("Audit tab passes capability filter to the API", async () => {
    const user = userEvent.setup();
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    const capInput = screen.getByRole("textbox", { name: /Filtrér på kapabilitet/i });
    await user.type(capInput, "issue.read");

    await waitFor(() => {
      const calls = mockAudit.mock.calls;
      const lastCall = calls[calls.length - 1];
      expect(lastCall?.[1]).toMatchObject({ capability: "issue.read" });
    });
  }, 15_000);

  it("Audit tab passes date range filters to the API", async () => {
    const user = userEvent.setup();
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    // Set a since date (datetime-local inputs)
    const sinceInput = screen.getByLabelText(/Fra dato/i);
    await user.type(sinceInput, "2026-05-01T00:00");

    await waitFor(() => {
      const calls = mockAudit.mock.calls;
      const lastCall = calls[calls.length - 1];
      expect(lastCall?.[1]?.since).toBeTruthy();
    });
  }, 15_000);

  it("Audit tab pagination: next/previous buttons are rendered and disabled when at bounds", async () => {
    const user = userEvent.setup();
    mockAudit.mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 });
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    await screen.findByText(/Ingen audit-hændelser/i);

    const prevBtn = screen.getByRole("button", { name: /Forrige/i });
    const nextBtn = screen.getByRole("button", { name: /Næste/i });
    expect(prevBtn).toBeDisabled();
    expect(nextBtn).toBeDisabled();
  }, 15_000);

  it("Audit tab pagination: next page fires API with incremented offset", async () => {
    const user = userEvent.setup();
    const pageOneItems = Array.from({ length: 50 }, (_, i) => ({
      id: `evt-${i}`,
      workspace_id: "ws-1",
      grant_id: null,
      action: "created",
      actor_id: "user-1",
      actor_name: "Alice",
      actor_type: "member",
      via: "ui",
      summary: `Event ${i}`,
      before: null,
      after: null,
      created_at: "2026-05-24T10:00:00Z",
    }));
    mockAudit.mockResolvedValueOnce({ items: pageOneItems, total: 120, limit: 50, offset: 0 });
    mockAudit.mockResolvedValueOnce({ items: [], total: 120, limit: 50, offset: 50 });

    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    // Wait for page 1 to load — multiple "Alice" entries are expected
    const aliceMatches = await screen.findAllByText("Alice");
    expect(aliceMatches.length).toBeGreaterThan(0);

    const nextBtn = screen.getByRole("button", { name: /Næste/i });
    expect(nextBtn).not.toBeDisabled();
    await user.click(nextBtn);

    await waitFor(() => {
      const calls = mockAudit.mock.calls;
      const offsets = calls.map((c) => c[1]?.offset);
      expect(offsets).toContain(50);
    });
  }, 15_000);

  it("Audit tab: clicking an entry shows detail panel", async () => {
    const user = userEvent.setup();
    const entry = {
      id: "evt-detail",
      workspace_id: "ws-1",
      grant_id: "grant-1",
      action: "created",
      actor_id: "user-1",
      actor_name: "Bob",
      actor_type: "member",
      via: "api",
      summary: "Grant oprettet",
      before: null,
      after: { capability: "issue.read" },
      created_at: "2026-05-24T10:00:00Z",
    };
    mockAudit.mockResolvedValue({ items: [entry], total: 1, limit: 50, offset: 0 });

    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    const row = await screen.findByText("Bob");
    await user.click(row.closest("button")!);

    // Detail panel should show — "Grant oprettet" appears in both list and detail panel
    expect(await screen.findByText("Detaljer")).toBeInTheDocument();
    expect(screen.getByText("api")).toBeInTheDocument();
    const summaryMatches = screen.getAllByText("Grant oprettet");
    expect(summaryMatches.length).toBeGreaterThan(0);
  }, 15_000);

  it("falls back gracefully when audit API returns malformed data", async () => {
    mockAudit.mockResolvedValueOnce({ broken: "data" });
    const user = userEvent.setup();
    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByRole("tab", { name: /Audit/i }));
    // parseWithFallback should return empty page — empty state shown
    await waitFor(() =>
      expect(screen.getByText(/Ingen audit-hændelser/i)).toBeInTheDocument(),
    );
  }, 15_000);
});
