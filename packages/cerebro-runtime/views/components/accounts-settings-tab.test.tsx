import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CerebroAccount } from "@multica/core/api";

const mockListAccounts = vi.hoisted(() => vi.fn());
const mockDeleteMutate = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("./use-cerebro-accounts", () => ({
  useCerebroAccountsList: mockListAccounts,
  useDeleteCerebroAccount: () => ({
    mutate: mockDeleteMutate,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

// The tab renders outside a workspace route in tests — pin the path builder
// to a fixed slug and flatten AppLink to a plain anchor (FIR-3118).
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

vi.mock("@multica/views/navigation", () => ({
  AppLink: ({
    href,
    children,
    className,
  }: {
    href: string;
    children: ReactNode;
    className?: string;
  }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

// Base UI's AlertDialog uses a portal that is awkward in jsdom — replace it
// with pass-through wrappers so the confirmation flow is observable. Mirrors
// the kill-switch-section test setup.
vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({
    children,
    open,
  }: {
    children: ReactNode;
    open: boolean;
  }) => (open ? <div role="dialog">{children}</div> : null),
  AlertDialogContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogHeader: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogTitle: ({ children }: { children: ReactNode }) => (
    <h1>{children}</h1>
  ),
  AlertDialogDescription: ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  ),
  AlertDialogFooter: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  AlertDialogAction: ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button type="button" onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
  AlertDialogCancel: ({
    children,
    disabled,
  }: {
    children: ReactNode;
    disabled?: boolean;
  }) => (
    <button type="button" disabled={disabled}>
      {children}
    </button>
  ),
}));

import { AccountsSettingsTab } from "./accounts-settings-tab";

const acc1: CerebroAccount = {
  id: "acc-1",
  workspace_id: "ws-1",
  provider: "claude",
  login_identity: "user-a@example.com",
  usage_window_pct: null,
  throttled_until: null,
  usage_5h_pct: null,
  usage_5h_resets_at: null,
  usage_7d_pct: null,
  usage_7d_resets_at: null,
  tokens_5h: 0,
  tokens_7d: 0,
  extra_spend_on: false,
  paused_manual: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  runtime_count: 1,
  available_runtime_count: 1,
  nearest_unpause_at: null,
  status: "available",
};
const acc2: CerebroAccount = { ...acc1, id: "acc-2", login_identity: "user-b@example.com" };

beforeEach(() => {
  mockListAccounts.mockReset();
  mockDeleteMutate.mockReset();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
});

describe("AccountsSettingsTab", () => {
  it("lists every workspace account with provider + login_identity", () => {
    mockListAccounts.mockReturnValue({ data: [acc1, acc2], isLoading: false });

    render(<AccountsSettingsTab />);

    expect(screen.getByText("user-a@example.com")).toBeInTheDocument();
    expect(screen.getByText("user-b@example.com")).toBeInTheDocument();
    expect(screen.getAllByText("claude")).toHaveLength(2);
  });

  it("links each row to the account detail page (FIR-3118)", () => {
    mockListAccounts.mockReturnValue({ data: [acc1], isLoading: false });

    render(<AccountsSettingsTab />);

    const link = screen.getByRole("link", { name: /user-a@example\.com/ });
    expect(link).toHaveAttribute("href", "/acme/settings/accounts/acc-1");
  });

  it("shows remaining usage for the 5h and weekly windows when reported (FIR-3118)", () => {
    mockListAccounts.mockReturnValue({
      data: [{ ...acc1, usage_5h_pct: 40, usage_7d_pct: 15 }],
      isLoading: false,
    });

    render(<AccountsSettingsTab />);

    expect(screen.getByText("Next 5h")).toBeInTheDocument();
    expect(screen.getByText("60% left")).toBeInTheDocument();
    expect(screen.getByText("This week")).toBeInTheDocument();
    expect(screen.getByText("85% left")).toBeInTheDocument();
  });

  it("does not relabel a weekly-only provider window as Next 5h (FIR-3118)", () => {
    mockListAccounts.mockReturnValue({
      data: [
        {
          ...acc1,
          provider: "codex",
          usage_window_pct: 73,
          usage_5h_pct: null,
          usage_7d_pct: 73,
        },
      ],
      isLoading: false,
    });

    render(<AccountsSettingsTab />);

    expect(
      within(screen.getByLabelText("Next 5h usage")).getByText("No data yet"),
    ).toBeInTheDocument();
    expect(
      within(screen.getByLabelText("This week usage")).getByText("27% left"),
    ).toBeInTheDocument();
  });

  it("keeps the legacy Next 5h fallback when no exact windows exist (FIR-3118)", () => {
    mockListAccounts.mockReturnValue({
      data: [{ ...acc1, usage_window_pct: 40 }],
      isLoading: false,
    });

    render(<AccountsSettingsTab />);

    expect(
      within(screen.getByLabelText("Next 5h usage")).getByText("60% left"),
    ).toBeInTheDocument();
    expect(
      within(screen.getByLabelText("This week usage")).getByText("No data yet"),
    ).toBeInTheDocument();
  });

  it("keeps remaining usage primary and shows token totals as the secondary line (FIR-3118)", () => {
    mockListAccounts.mockReturnValue({
      data: [
        {
          ...acc1,
          usage_5h_pct: 40,
          tokens_5h: 1_500,
          tokens_7d: 2_400_000,
        },
      ],
      isLoading: false,
    });

    render(<AccountsSettingsTab />);

    expect(screen.getByText("60% left")).toBeInTheDocument();
    expect(screen.getByText("No data yet")).toBeInTheDocument();
    expect(screen.getByText("1.5k tok used")).toBeInTheDocument();
    expect(screen.getByText("2.4M tok used")).toBeInTheDocument();
  });

  it("shows the empty state when no accounts are registered", () => {
    mockListAccounts.mockReturnValue({ data: [], isLoading: false });

    render(<AccountsSettingsTab />);

    expect(
      screen.getByText(/No accounts registered yet/),
    ).toBeInTheDocument();
  });

  it("opens a confirmation dialog before calling delete and forwards on confirm", async () => {
    const user = userEvent.setup();
    mockListAccounts.mockReturnValue({ data: [acc1], isLoading: false });
    mockDeleteMutate.mockImplementation(
      (_id: string, opts?: { onSuccess?: () => void }) => opts?.onSuccess?.(),
    );

    render(<AccountsSettingsTab />);

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: /Delete account user-a@example\.com/ }),
    );
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(mockDeleteMutate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mockDeleteMutate).toHaveBeenCalledWith("acc-1", expect.any(Object));
    });
    expect(mockToastSuccess).toHaveBeenCalledWith(
      expect.stringContaining("user-a@example.com"),
    );
  });

  it("surfaces server errors via toast.error", async () => {
    const user = userEvent.setup();
    mockListAccounts.mockReturnValue({ data: [acc1], isLoading: false });
    mockDeleteMutate.mockImplementation(
      (_id: string, opts?: { onError?: (e: Error) => void }) =>
        opts?.onError?.(new Error("permission denied")),
    );

    render(<AccountsSettingsTab />);
    await user.click(
      screen.getByRole("button", { name: /Delete account user-a@example\.com/ }),
    );
    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("permission denied");
    });
  });
});
