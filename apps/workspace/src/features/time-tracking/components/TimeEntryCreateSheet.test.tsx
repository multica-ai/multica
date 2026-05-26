import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { TimeEntryCreateSheet } from "./TimeEntryCreateSheet";

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/components/ui/sheet", () => ({
  Sheet: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div>{children}</div> : null,
  SheetContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("@/components/ui/date-time-picker", () => ({
  DateTimePicker: ({ placeholder }: { placeholder: string }) => <div>{placeholder}</div>,
}));

vi.mock("@/features/issues", () => ({
  useIssueStore: (selector: (state: { issues: Array<{ id: string; identifier: string; title: string }> }) => unknown) =>
    selector({
      issues: [
        { id: "issue-1", identifier: "MUL-1", title: "First issue" },
        { id: "issue-2", identifier: "MUL-2", title: "Second issue" },
      ],
    }),
}));

vi.mock("@/features/issues/queries", () => ({
  useIssuesListQuery: () => ({
    data: {
      issues: [
        { id: "issue-1", identifier: "MUL-1", title: "First issue" },
        { id: "issue-2", identifier: "MUL-2", title: "Second issue" },
      ],
    },
  }),
}));

vi.mock("@/features/issues/components/pickers/property-picker", () => ({
  PropertyPicker: ({
    trigger,
    children,
  }: {
    trigger: React.ReactNode;
    children: React.ReactNode;
  }) => (
    <div>
      <div data-testid="issue-trigger">{trigger}</div>
      <div>{children}</div>
    </div>
  ),
  PickerEmpty: () => <div>No results</div>,
  PickerItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("../hooks/use-time-tracking", () => ({
  useCurrentTimerQuery: () => ({ data: null }),
}));

vi.mock("../hooks/use-time-entry-actions", () => ({
  useTimeEntryActions: () => ({
    createHistoricalEntry: vi.fn(),
  }),
}));

describe("TimeEntryCreateSheet", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("updates the default issue when the sheet opens with a new issue context", () => {
    const { rerender } = render(
      <TimeEntryCreateSheet open defaultIssueId="issue-1" onClose={vi.fn()} />,
    );

    expect(screen.getByText("MUL-1 · First issue")).toBeInTheDocument();

    rerender(<TimeEntryCreateSheet open defaultIssueId="issue-2" onClose={vi.fn()} />);

    expect(screen.getByText("MUL-2 · Second issue")).toBeInTheDocument();
  });
});
