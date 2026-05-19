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
  capability: "issue.read",
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
  mockGetGrant.mockReset();
  mockCreateGrant.mockReset();
  mockUpdateGrant.mockReset();
  mockDeleteGrant.mockReset();
  mockUseFeatureFlag.mockReturnValue(true);
  mockUseCurrentMember.mockReturnValue({
    userId: "user-1",
    role: "admin" as const,
    member: null,
    isLoading: false,
  });
});

describe("PermissionsPage", () => {
  it("renders the direct route for admins even when the nav feature flag is off", async () => {
    mockUseFeatureFlag.mockReturnValueOnce(false);
    mockListGrants.mockResolvedValueOnce({
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const { ui } = makePage();
    render(ui);
    expect(await screen.findByText(/Ingen grants matcher/i)).toBeInTheDocument();
    expect(mockListGrants).toHaveBeenCalled();
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
    expect(screen.getByText("issue.read")).toBeInTheDocument();
    // Subject-type chip
    expect(screen.getByText("group")).toBeInTheDocument();
  });

  it("revokes an existing grant from the drawer instead of hard-deleting it", async () => {
    const user = userEvent.setup();
    mockListGrants.mockResolvedValue({
      items: [sampleGrant],
      total: 1,
      limit: 50,
      offset: 0,
    });
    mockGetGrant.mockResolvedValue(sampleGrant);
    mockUpdateGrant.mockResolvedValue({
      ...sampleGrant,
      status: "revoked",
      updated_at: "2026-05-13T10:02:00Z",
    });

    const { ui } = makePage();
    render(ui);

    await user.click(await screen.findByText("Marketing"));
    await user.click(
      await screen.findByRole("button", { name: /Tilbagekald grant/i }),
    );
    await user.click(screen.getByRole("button", { name: /^Tilbagekald$/i }));

    await waitFor(() =>
      expect(mockUpdateGrant).toHaveBeenCalledWith("ws-1", "grant-1", {
        resource_pattern: undefined,
        capability: undefined,
        classification_ceiling: undefined,
        time_window_start: null,
        time_window_end: null,
        approval_required: undefined,
        status: "revoked",
      }),
    );
    expect(mockDeleteGrant).not.toHaveBeenCalled();
  }, 10_000);

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
    // Capability select defaults to the canonical issue-read capability.
    expect(screen.getByRole("combobox", { name: "Kapabilitet" })).toHaveTextContent(
      "issue.read",
    );
  }, 10_000);

  it("create-grant dialog renders a submit-button form so create fires on submit", async () => {
    // Regression: JEH-1529 — earlier code wired `onClick={handleSubmit}`
    // directly on a free-standing button, which silently swallowed the
    // click in real browsers (Tines QA: dialog closed but no POST hit
    // the backend). The fix is to mount the inputs in a real `<form>`
    // with `onSubmit={handleSubmit}` and a `type="submit"` button so
    // both pointer clicks and the Enter key always trigger the mutation.
    // This test asserts the structural contract; the actual submission
    // path is exercised end-to-end by QA because Base UI's Input +
    // portal + Select internals make jsdom's pointer simulation
    // unreliable for the click-then-mutate path.
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
    await screen.findByRole("heading", { name: /Nyt grant/i });

    const submitBtn = screen.getByRole("button", {
      name: /^Opret grant$/,
    }) as HTMLButtonElement;
    expect(submitBtn.type).toBe("submit");
    const form = submitBtn.closest("form");
    expect(form).not.toBeNull();
    expect(form?.tagName).toBe("FORM");
  }, 10_000);

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

  it("renders 'Adgang nægtet' when the current member is not admin/owner", async () => {
    mockUseCurrentMember.mockReturnValueOnce({
      userId: "user-1",
      role: "member" as const,
      member: null,
      isLoading: false,
    });
    const { ui } = makePage();
    render(ui);
    expect(
      await screen.findByText(/Adgang nægtet/i),
    ).toBeInTheDocument();
    expect(mockListGrants).not.toHaveBeenCalled();
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
