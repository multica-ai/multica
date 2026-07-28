import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockListServiceTokens = vi.hoisted(() => vi.fn());
const mockCreateServiceToken = vi.hoisted(() => vi.fn());
const mockRevokeServiceToken = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockCopyText = vi.hoisted(() => vi.fn(() => Promise.resolve(true)));

// Role is driven per-test through this mutable holder so a single member-list
// mock can flip between manager and non-manager.
const roleHolder = vi.hoisted(() => ({ role: "admin" as string | undefined }));

// AlertDialog + Dialog are Base UI portals that are awkward in jsdom — strip to
// pass-through wrappers so the confirm/reveal flows are observable.
vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div role="dialog">{children}</div> : null,
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogAction: ({
    children,
    onClick,
  }: {
    children: ReactNode;
    onClick?: () => void;
    variant?: string;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  AlertDialogCancel: ({ children }: { children: ReactNode }) => (
    <button type="button">{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div role="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

// Tooltip's TooltipTrigger uses a `render` prop; unwrap it to the raw element.
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => render,
  TooltipContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listServiceTokens: mockListServiceTokens,
    createServiceToken: mockCreateServiceToken,
    revokeServiceToken: mockRevokeServiceToken,
  },
}));

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: mockCopyText }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [{ user_id: "user-1", role: roleHolder.role }] }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { ServiceTokensSection } from "./service-tokens-section";

beforeEach(() => {
  mockListServiceTokens.mockReset();
  mockCreateServiceToken.mockReset();
  mockRevokeServiceToken.mockReset();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
  mockCopyText.mockClear();
  roleHolder.role = "admin";
  mockListServiceTokens.mockResolvedValue([]);
});

describe("ServiceTokensSection", () => {
  it("offers only read scopes and mandatory expiry choices", async () => {
    render(<ServiceTokensSection />);

    expect(await screen.findByRole("button", { name: "skills:read" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "agents:read" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "issues:read" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /:write$/ })).not.toBeInTheDocument();
    expect(screen.queryByText(/no expiry/i)).not.toBeInTheDocument();
  });

  it("shows an empty state before the first service token", async () => {
    render(<ServiceTokensSection />);
    expect(await screen.findByText("No service tokens yet.")).toBeInTheDocument();
  });

  it("shows an owner/admin-only note and never lists tokens for non-managers", async () => {
    roleHolder.role = "member";
    render(<ServiceTokensSection />);

    expect(
      await screen.findByText(/only workspace owners and admins can manage service tokens/i),
    ).toBeInTheDocument();
    expect(mockListServiceTokens).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: /^create$/i })).not.toBeInTheDocument();
  });

  it("loads and renders existing tokens for a manager", async () => {
    mockListServiceTokens.mockResolvedValue([
      {
        id: "t1",
        name: "Atlas read-only",
        token_prefix: "msv_abc",
        scopes: ["skills:read"],
        expires_at: null,
        last_used_at: null,
        revoked: false,
        created_at: "2026-07-01T00:00:00Z",
      },
    ]);

    render(<ServiceTokensSection />);

    expect(await screen.findByText("Atlas read-only")).toBeInTheDocument();
    expect(screen.getByText(/scopes: skills:read/)).toBeInTheDocument();
    expect(mockListServiceTokens).toHaveBeenCalledTimes(1);
  });

  it("disables Create until a name and at least one scope are present", async () => {
    const user = userEvent.setup();
    render(<ServiceTokensSection />);

    await screen.findByPlaceholderText(/token name/i);
    const createBtn = screen.getByRole("button", { name: /^create$/i });
    // Default selected scope is skills:read, but the name is empty.
    expect(createBtn).toBeDisabled();

    await user.type(screen.getByPlaceholderText(/token name/i), "CI reader");
    expect(createBtn).toBeEnabled();

    // Deselecting the only scope disables Create again.
    await user.click(screen.getByRole("button", { name: "skills:read" }));
    expect(createBtn).toBeDisabled();
  });

  it("creates a token with the selected scopes and reveals the secret once", async () => {
    const user = userEvent.setup();
    mockCreateServiceToken.mockResolvedValue({
      id: "t2",
      name: "CI reader",
      token_prefix: "msv_xyz",
      scopes: ["skills:read", "issues:read"],
      expires_at: null,
      last_used_at: null,
      revoked: false,
      created_at: "2026-07-02T00:00:00Z",
      token: "msv_supersecretrawvalue",
    });

    render(<ServiceTokensSection />);
    await screen.findByPlaceholderText(/token name/i);

    await user.type(screen.getByPlaceholderText(/token name/i), "CI reader");
    await user.click(screen.getByRole("button", { name: "issues:read" }));
    await user.click(screen.getByRole("button", { name: /^create$/i }));

    await waitFor(() => {
      expect(mockCreateServiceToken).toHaveBeenCalledWith({
        name: "CI reader",
        scopes: ["skills:read", "issues:read"],
        expires_in_days: 90,
      });
    });

    expect(await screen.findByText("msv_supersecretrawvalue")).toBeInTheDocument();
    expect(screen.getByText(/it will not be shown again/i)).toBeInTheDocument();
  });

  it("revokes a token only after explicit confirmation", async () => {
    const user = userEvent.setup();
    mockListServiceTokens.mockResolvedValue([
      {
        id: "t1",
        name: "Atlas read-only",
        token_prefix: "msv_abc",
        scopes: ["skills:read"],
        expires_at: null,
        last_used_at: null,
        revoked: false,
        created_at: "2026-07-01T00:00:00Z",
      },
    ]);
    mockRevokeServiceToken.mockResolvedValue(undefined);

    render(<ServiceTokensSection />);
    await screen.findByText("Atlas read-only");

    await user.click(screen.getByRole("button", { name: /revoke atlas read-only/i }));
    // Confirmation dialog appears; nothing revoked yet.
    expect(await screen.findByText(/revoke service token\?/i)).toBeInTheDocument();
    expect(mockRevokeServiceToken).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: /^revoke$/i }));
    await waitFor(() => {
      expect(mockRevokeServiceToken).toHaveBeenCalledWith("t1");
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Service token revoked");
  });

  it("surfaces a load error via toast.error", async () => {
    mockListServiceTokens.mockRejectedValue(new Error("network down"));
    render(<ServiceTokensSection />);
    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("network down");
    });
  });
});
