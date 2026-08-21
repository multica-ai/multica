import type { ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const mockSetLastMode = vi.hoisted(() => vi.fn());

const mockCreateModeStore = {
  lastMode: "agent" as "agent" | "manual",
  setLastMode: mockSetLastMode,
};

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: Object.assign(
    (selector: (s: typeof mockCreateModeStore) => unknown) =>
      selector(mockCreateModeStore),
    { getState: () => mockCreateModeStore },
  ),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({
    className,
    children,
  }: {
    className?: string;
    children: ReactNode;
  }) => (
    <div data-testid="dialog-content" className={className}>
      {children}
    </div>
  ),
}));

vi.mock("./quick-create-issue", () => ({
  AgentCreatePanel: () => <div>agent panel</div>,
}));

vi.mock("./create-issue", () => ({
  ManualCreatePanel: () => <div>manual panel</div>,
  manualDialogContentClass: () => "manual-dialog-class",
}));

// `cn` is deliberately NOT mocked here: the whole point of these assertions is
// that tailwind-merge keeps the phone cap and the `sm:` width as two separate
// groups instead of collapsing them into one max-width.
import { CreateIssueDialog } from "./create-issue-dialog";

function contentClass() {
  return screen.getByTestId("dialog-content").className;
}

describe("CreateIssueDialog sizing", () => {
  // MUL-6236: every width the shell sets is `!important` so it can beat
  // DialogContent's own sizing — which also beat DialogContent's
  // `max-w-[calc(100%-2rem)]` gutter, so the card ran the full width of a
  // phone screen with no margin on either side.
  it("caps the agent dialog inside the viewport on phones", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="agent" />);

    expect(contentClass()).toContain("!max-w-[calc(100vw-1.5rem)]");
    expect(contentClass()).toContain("sm:!max-w-xl");
  });

  it("uses dvh so mobile browser chrome cannot hide the agent footer", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="agent" />);

    expect(contentClass()).toContain("!max-h-[80dvh]");
    expect(contentClass()).not.toContain("!max-h-[80vh]");
  });

  it("hands manual mode its own sizing", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="manual" />);

    expect(contentClass()).toBe("manual-dialog-class");
  });
});
