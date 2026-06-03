import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MobileWorkspaceToolbar } from "./mobile-workspace-toolbar";

const mocks = vi.hoisted(() => ({
  pathname: "/projects/project-1",
  openModal: vi.fn(),
  hasDraft: false,
}));

vi.mock("@/components/ui/sidebar", () => ({
  SidebarTrigger: (props: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props} />
  ),
}));

vi.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
  TooltipTrigger: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
}));

vi.mock("@/features/modals", () => ({
  useModalStore: {
    getState: () => ({
      open: mocks.openModal,
    }),
  },
}));

vi.mock("@/features/issues/stores/draft-store", () => ({
  useIssueDraftStore: (selector: (state: { draft: { title: string; description: string } }) => unknown) =>
    selector({
      draft: mocks.hasDraft
        ? { title: "Draft issue", description: "" }
        : { title: "", description: "" },
    }),
}));

vi.mock("@/features/time-tracking", () => ({
  PomodoroStatusPill: () => <a href="/pomodoro">Break 05:00</a>,
}));

vi.mock("@/shared/router", () => ({
  usePathname: () => mocks.pathname,
}));

describe("MobileWorkspaceToolbar", () => {
  beforeEach(() => {
    mocks.pathname = "/projects/project-1";
    mocks.hasDraft = false;
    mocks.openModal.mockClear();
  });

  it("renders the current page title and pomodoro status", () => {
    render(<MobileWorkspaceToolbar />);

    expect(screen.getByText("Page")).toBeInTheDocument();
    expect(screen.getByText("Projects")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Break 05:00" })).toHaveAttribute("href", "/pomodoro");
  });

  it("opens the mobile sidebar and create issue action controls", () => {
    render(<MobileWorkspaceToolbar />);

    expect(screen.getByRole("button", { name: "Open navigation" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "New issue" }));

    expect(mocks.openModal).toHaveBeenCalledWith("create-issue");
  });

  it("maps inbox title for the root route", () => {
    mocks.pathname = "/";

    render(<MobileWorkspaceToolbar />);

    expect(screen.getByText("Inbox")).toBeInTheDocument();
  });
});
