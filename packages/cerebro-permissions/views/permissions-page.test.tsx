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
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", name: "Acme" }),
}));

const mockUseFeatureFlag = vi.hoisted(() => vi.fn(() => true));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mockUseFeatureFlag,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { PermissionsPage } from "./permissions-page";

function makePage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    qc,
    ui: (
      <QueryClientProvider client={qc}>
        <PermissionsPage />
      </QueryClientProvider>
    ),
  };
}

const sampleGrant = {
  id: "grant-1",
  workspace_id: "ws-1",
  subject: {
    type: "group",
    id: "g-marketing",
    display_name: "Marketing",
    avatar_url: null,
  },
  resource: { type: "issue", pattern: "*" },
  capability: "issues.read",
  classification_ceiling: "internal",
  status: "active",
  approval_required: false,
  time_window: null,
  description: null,
  created_by: null,
  created_at: "2026-05-13T10:00:00Z",
  updated_at: "2026-05-13T10:00:00Z",
};

beforeEach(() => {
  mockListGrants.mockReset();
  mockCreateGrant.mockReset();
  mockUseFeatureFlag.mockReturnValue(true);
});

describe("PermissionsPage", () => {
  it("returns null when cerebro_persona_permissions flag is off", () => {
    mockUseFeatureFlag.mockReturnValueOnce(false);
    mockListGrants.mockResolvedValueOnce({
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    const { container } = render(ui);
    expect(container.firstChild).toBeNull();
  });

  it("renders the empty state with no grants", async () => {
    mockListGrants.mockResolvedValueOnce({
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);

    await waitFor(() =>
      expect(screen.getByText(/Ingen grants matcher/i)).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: /Nyt grant/i })).toBeInTheDocument();
  });

  it("renders a row for each grant", async () => {
    mockListGrants.mockResolvedValueOnce({
      items: [sampleGrant],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);

    expect(await screen.findByText("Marketing")).toBeInTheDocument();
    expect(screen.getByText("issues.read")).toBeInTheDocument();
    // Subject-type chip
    expect(screen.getByText("group")).toBeInTheDocument();
  });

  it("opens the create-grant dialog when 'Nyt grant' is clicked", async () => {
    const user = userEvent.setup();
    mockListGrants.mockResolvedValueOnce({
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);

    await user.click(screen.getByRole("button", { name: /Nyt grant/i }));
    expect(
      await screen.findByRole("heading", { name: /Nyt grant/i }),
    ).toBeInTheDocument();
    // Capability input is required-ish; presence proves the form mounted
    expect(screen.getByPlaceholderText("issues.read")).toBeInTheDocument();
  });

  it("falls back to an empty list when the API returns malformed data", async () => {
    // parseWithFallback should swallow the bad shape and the page should
    // still render — the boundary defense from CLAUDE.md.
    mockListGrants.mockResolvedValueOnce({ broken: true });
    const { ui } = makePage();
    render(ui);

    await waitFor(() =>
      expect(screen.getByText(/Ingen grants matcher/i)).toBeInTheDocument(),
    );
  });

  it("switches to the Audit tab when clicked", async () => {
    const user = userEvent.setup();
    mockListGrants.mockResolvedValueOnce({
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);

    await user.click(screen.getByRole("tab", { name: "Audit" }));
    expect(await screen.findByText(/Ingen audit-hændelser/i)).toBeInTheDocument();
  });
});
