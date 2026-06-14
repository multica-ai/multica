import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { FocusPage } from "./FocusPage";
import type { FocusSession } from "@/shared/types";

const completeQuickStartMutate = vi.fn();

const quickStartSession: FocusSession = {
  id: "focus-1",
  mode: "quick_start",
  phase: "focusing",
  preset: "two_minute_start",
  issue_id: null,
  description: "Read the brief",
  commitment_text: "Open the brief",
  label_ids: [],
  first_started_at: "2026-06-13T09:57:50.000Z",
  started_at: "2026-06-13T09:57:50.000Z",
  paused_at: null,
  elapsed_focus_seconds: 0,
  suggested_break_seconds: null,
  status_reason: null,
  reason_note: null,
};

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: (selector: (state: { issues: never[] }) => unknown) => selector({ issues: [] }),
}));

vi.mock("@/features/issues/queries", () => ({
  useIssuesListQuery: () => ({ data: { issues: [] } }),
}));

vi.mock("@/features/issues/components/pickers/property-picker", () => ({
  PropertyPicker: ({ trigger, children }: { trigger: React.ReactNode; children: React.ReactNode }) => (
    <div>
      <button type="button">{trigger}</button>
      <div>{children}</div>
    </div>
  ),
  PickerEmpty: () => <div>No issues</div>,
  PickerItem: ({ children, onClick }: { children: React.ReactNode; onClick: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("../components/time-entry-label-picker", () => ({
  TimeEntryLabelPicker: () => <div>Label picker</div>,
}));

vi.mock("../hooks/use-time-tracking", () => ({
  useTimeEntryLabelsQuery: () => ({ data: [] }),
  useTimeEntryLabelMutations: () => ({ createTimeEntryLabel: vi.fn() }),
}));

vi.mock("../hooks/use-focus", () => ({
  useFocusQuery: () => ({ data: quickStartSession, isLoading: false }),
  useStartFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  usePauseFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useResumeFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useCompleteQuickStartMutation: () => ({
    mutate: completeQuickStartMutate,
    isPending: false,
  }),
  useCompleteFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useAbandonFocusMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useStartFocusBreakMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useSkipFocusBreakMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useCompleteFocusBreakMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

describe("FocusPage quick start", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-13T10:00:00.000Z"));
    completeQuickStartMutate.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the completed two-minute start state and continues as Flowtime", () => {
    render(<FocusPage />);

    expect(screen.getByText("Quick start remaining")).toBeInTheDocument();
    expect(screen.getByText("0:00")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /continue flowtime/i }));

    expect(completeQuickStartMutate).toHaveBeenCalledTimes(1);
    expect(completeQuickStartMutate).toHaveBeenCalledWith(undefined, expect.any(Object));
  });
});
