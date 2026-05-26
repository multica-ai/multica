import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { GlobalTimerWidget } from "./GlobalTimerWidget";

const mockToastError = vi.fn();
const mockRequestStart = vi.fn();
const mockConfirmSwitch = vi.fn();
const mockSetPendingSwitch = vi.fn();

let mockCurrentEntry: object | null = null;
let mockPendingSwitch: object | null = null;
// Mutable pathname used by the @/shared/router mock so individual tests can
// simulate being on a specific route without requiring a real router context.
let mockPathname = "/";

vi.mock("@/shared/router", () => ({
  usePathname: () => mockPathname,
}));

vi.mock("sonner", () => ({
  toast: {
    error: (...args: unknown[]) => mockToastError(...args),
  },
}));

vi.mock("@/components/ui/sidebar", () => ({
  SidebarMenuButton: ({
    children,
    onClick,
    className,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    className?: string;
  }) => (
    <button type="button" className={className} onClick={onClick}>
      {children}
    </button>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("./PomodoroTimer", () => ({
  PomodoroTimer: () => <div>Pomodoro</div>,
}));

vi.mock("../hooks/use-time-tracking", () => ({
  useCurrentTimerQuery: () => ({ data: mockCurrentEntry }),
  useStopTimerMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));

vi.mock("../hooks/use-time-entry-actions", () => ({
  useTimeEntryActions: () => ({
    requestStart: mockRequestStart,
    pendingSwitch: mockPendingSwitch,
    confirmSwitch: mockConfirmSwitch,
    setPendingSwitch: mockSetPendingSwitch,
  }),
}));

describe("GlobalTimerWidget", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCurrentEntry = null;
    mockPendingSwitch = null;
    // Reset pathname to a non-pomodoro route for each test.
    mockPathname = "/";
    // Ensure pomodoro-mode is off so tests start in the normal timer state.
    localStorage.removeItem("pomodoro-mode");
  });

  it("keeps the typed description when start only stages a switch", async () => {
    mockRequestStart.mockResolvedValue(null);

    render(<GlobalTimerWidget />);

    fireEvent.click(screen.getByRole("button", { name: /track time/i }));

    const input = screen.getByPlaceholderText("What are you working on?");
    fireEvent.change(input, { target: { value: "Write docs" } });
    fireEvent.click(screen.getByRole("button", { name: /^start$/i }));

    await waitFor(() => {
      expect(mockRequestStart).toHaveBeenCalledTimes(1);
    });

    expect(screen.getByDisplayValue("Write docs")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^start$/i })).toBeInTheDocument();
  });

  it("disables the start button while the start request is in flight", async () => {
    let resolveStart!: (value: null) => void;
    mockRequestStart.mockReturnValue(
      new Promise<null>((resolve) => {
        resolveStart = resolve;
      }),
    );

    render(<GlobalTimerWidget />);

    fireEvent.click(screen.getByRole("button", { name: /track time/i }));
    fireEvent.change(screen.getByPlaceholderText("What are you working on?"), {
      target: { value: "Write docs" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^start$/i }));

    expect(screen.getByRole("button", { name: /^start$/i })).toBeDisabled();

    resolveStart(null);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /^start$/i })).not.toBeDisabled();
    });
  });

  it("shows a toast when switch confirmation fails", async () => {
    mockPendingSwitch = { start_time: "2026-05-17T10:00:00Z" };
    mockConfirmSwitch.mockRejectedValue(new Error("switch failed"));

    render(<GlobalTimerWidget />);

    fireEvent.click(screen.getByRole("button", { name: /track time/i }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm switch" }));

    await waitFor(() => {
      expect(mockConfirmSwitch).toHaveBeenCalledTimes(1);
    });

    expect(mockToastError).toHaveBeenCalledWith("Failed to switch timer");
  });

  // ── Dual-PomodoroTimer suppression ──────────────────────────────────────────
  // When pomodoroMode=true AND the path starts with /pomodoro, the compact
  // PomodoroTimer must be suppressed so two live timer instances never coexist
  // (which would cause double-completion via shared session state).
  it("suppresses the compact PomodoroTimer when pomodoroMode is on and path is /pomodoro", () => {
    localStorage.setItem("pomodoro-mode", "true");
    mockPathname = "/pomodoro";

    render(<GlobalTimerWidget />);

    // The mock PomodoroTimer renders text "Pomodoro" — it must NOT appear.
    expect(screen.queryByText("Pomodoro")).not.toBeInTheDocument();
    // The mode-switch button must still be present so the user can exit.
    expect(screen.getByRole("button", { name: /切换为普通计时/i })).toBeInTheDocument();
  });

  it("renders the compact PomodoroTimer when pomodoroMode is on but path is not /pomodoro", () => {
    localStorage.setItem("pomodoro-mode", "true");
    mockPathname = "/issues";

    render(<GlobalTimerWidget />);

    // Compact PomodoroTimer IS rendered on non-pomodoro routes.
    expect(screen.getByText("Pomodoro")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /切换为普通计时/i })).toBeInTheDocument();
  });
});
