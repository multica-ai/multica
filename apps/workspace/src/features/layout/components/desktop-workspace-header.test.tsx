import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DesktopWorkspaceHeader } from "./desktop-workspace-header";

const mocks = vi.hoisted(() => ({
  pathname: "/issues/issue-123",
  openSearch: vi.fn(),
  openModal: vi.fn(),
  hasDraft: false,
}));

vi.mock("@/features/search", () => ({
  useSearchStore: {
    getState: () => ({
      open: mocks.openSearch,
    }),
  },
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
  PomodoroStatusPill: () => <a href="/pomodoro">Focus 23:00</a>,
}));

vi.mock("@/shared/router", () => ({
  usePathname: () => mocks.pathname,
}));

describe("DesktopWorkspaceHeader", () => {
  beforeEach(() => {
    mocks.pathname = "/issues/issue-123";
    mocks.hasDraft = false;
    mocks.openSearch.mockClear();
    mocks.openModal.mockClear();
  });

  it("renders the current page title and pomodoro status", () => {
    render(<DesktopWorkspaceHeader />);

    expect(screen.getByRole("heading", { name: "Issues" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Focus 23:00" })).toHaveAttribute("href", "/pomodoro");
  });

  it("opens global search and the create issue modal from desktop actions", () => {
    render(<DesktopWorkspaceHeader />);

    fireEvent.click(screen.getByRole("button", { name: /search/i }));
    fireEvent.click(screen.getByRole("button", { name: /new issue/i }));

    expect(mocks.openSearch).toHaveBeenCalledTimes(1);
    expect(mocks.openModal).toHaveBeenCalledWith("create-issue");
  });

  it("maps page title from pathname changes", () => {
    mocks.pathname = "/pomodoro";

    render(<DesktopWorkspaceHeader />);

    expect(screen.getByRole("heading", { name: "Pomodoro" })).toBeInTheDocument();
  });
});
