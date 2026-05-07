import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockPauseWorkspaceTasks = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());

// AlertDialog is a Base UI portal that's awkward in jsdom — strip it to
// pass-through wrappers so the confirmation flow is observable.
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

vi.mock("@multica/core/api", () => ({
  api: { pauseWorkspaceTasks: mockPauseWorkspaceTasks },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: mockSetQueryData }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { KillSwitchSection } from "./kill-switch-section";
import type { Workspace } from "@multica/core/types";

const baseWorkspace: Workspace = {
  id: "ws-1",
  name: "Test Workspace",
  slug: "test",
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "TEST",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

beforeEach(() => {
  mockPauseWorkspaceTasks.mockReset();
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
  mockSetQueryData.mockClear();
});

describe("KillSwitchSection", () => {
  it("renders an unchecked switch by default with non-paused copy", () => {
    render(<KillSwitchSection workspace={baseWorkspace} canManage />);
    const switchEl = screen.getByRole("switch", { name: /pause all agent tasks/i });
    expect(switchEl).toHaveAttribute("aria-checked", "false");
    expect(
      screen.queryByText(/agent tasks are paused workspace-wide/i),
    ).not.toBeInTheDocument();
  });

  it("renders a checked switch and warning copy when settings.tasks_paused is true", () => {
    render(
      <KillSwitchSection
        workspace={{ ...baseWorkspace, settings: { tasks_paused: true } }}
        canManage
      />,
    );
    expect(screen.getByRole("switch", { name: /pause all agent tasks/i })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(
      screen.getByText(/agent tasks are paused workspace-wide/i),
    ).toBeInTheDocument();
  });

  it("disables the switch and shows a hint when the user cannot manage", () => {
    render(<KillSwitchSection workspace={baseWorkspace} canManage={false} />);
    expect(screen.getByRole("switch", { name: /pause all agent tasks/i })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(
      screen.getByText(/Only admins and owners can toggle the emergency stop/i),
    ).toBeInTheDocument();
  });

  it("opens a confirmation dialog before engaging the kill switch", async () => {
    const user = userEvent.setup();
    render(<KillSwitchSection workspace={baseWorkspace} canManage />);

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: /pause all agent tasks/i }));

    // Engage requires explicit confirmation.
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText(/Engage emergency stop\?/i)).toBeInTheDocument();
    expect(mockPauseWorkspaceTasks).not.toHaveBeenCalled();
  });

  it("calls pauseWorkspaceTasks(true), toasts cancelled count, and updates the cache after confirm", async () => {
    const user = userEvent.setup();
    const updatedWorkspace = {
      ...baseWorkspace,
      settings: { tasks_paused: true },
    };
    mockPauseWorkspaceTasks.mockResolvedValue({
      workspace: updatedWorkspace,
      cancelled_count: 3,
    });

    render(<KillSwitchSection workspace={baseWorkspace} canManage />);
    await user.click(screen.getByRole("switch", { name: /pause all agent tasks/i }));
    await user.click(screen.getByRole("button", { name: /stop all tasks/i }));

    await waitFor(() => {
      expect(mockPauseWorkspaceTasks).toHaveBeenCalledWith("ws-1", true);
    });
    expect(mockToastSuccess).toHaveBeenCalledWith(
      expect.stringContaining("cancelled 3 tasks"),
    );
    expect(mockSetQueryData).toHaveBeenCalledTimes(1);
  });

  it("uses singular wording when exactly one task is cancelled", async () => {
    const user = userEvent.setup();
    mockPauseWorkspaceTasks.mockResolvedValue({
      workspace: { ...baseWorkspace, settings: { tasks_paused: true } },
      cancelled_count: 1,
    });

    render(<KillSwitchSection workspace={baseWorkspace} canManage />);
    await user.click(screen.getByRole("switch", { name: /pause all agent tasks/i }));
    await user.click(screen.getByRole("button", { name: /stop all tasks/i }));

    await waitFor(() => {
      expect(mockToastSuccess).toHaveBeenCalledWith(
        expect.stringContaining("cancelled 1 task"),
      );
    });
    expect(mockToastSuccess).toHaveBeenCalledWith(
      expect.not.stringContaining("1 tasks"),
    );
  });

  it("releases the switch immediately without a confirmation dialog", async () => {
    const user = userEvent.setup();
    mockPauseWorkspaceTasks.mockResolvedValue({
      workspace: baseWorkspace,
      cancelled_count: 0,
    });

    render(
      <KillSwitchSection
        workspace={{ ...baseWorkspace, settings: { tasks_paused: true } }}
        canManage
      />,
    );
    await user.click(screen.getByRole("switch", { name: /pause all agent tasks/i }));

    await waitFor(() => {
      expect(mockPauseWorkspaceTasks).toHaveBeenCalledWith("ws-1", false);
    });
    // No confirmation dialog should ever appear for the release path.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mockToastSuccess).toHaveBeenCalledWith("Emergency stop released");
  });

  it("surfaces API errors via toast.error and skips the cache update", async () => {
    const user = userEvent.setup();
    mockPauseWorkspaceTasks.mockRejectedValue(new Error("network down"));

    render(<KillSwitchSection workspace={baseWorkspace} canManage />);
    await user.click(screen.getByRole("switch", { name: /pause all agent tasks/i }));
    await user.click(screen.getByRole("button", { name: /stop all tasks/i }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("network down");
    });
    expect(mockSetQueryData).not.toHaveBeenCalled();
  });
});
