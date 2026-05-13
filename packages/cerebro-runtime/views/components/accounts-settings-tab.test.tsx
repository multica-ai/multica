import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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

  it("shows the empty state when no accounts are registered", () => {
    mockListAccounts.mockReturnValue({ data: [], isLoading: false });

    render(<AccountsSettingsTab />);

    expect(
      screen.getByText(/Ingen konti registreret endnu/),
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
      screen.getByRole("button", { name: /Slet konto user-a@example\.com/ }),
    );
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(mockDeleteMutate).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Slet" }));

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
      screen.getByRole("button", { name: /Slet konto user-a@example\.com/ }),
    );
    await user.click(screen.getByRole("button", { name: "Slet" }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("permission denied");
    });
  });
});
