import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { CompactNavSheet } from "./compact-nav-sheet";

const state = vi.hoisted(() => ({
  pathname: "/acme/issues",
  isCompact: true,
  openMobile: false,
  setOpenMobile: vi.fn(),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: state.pathname }),
}));
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  useSidebar: () => ({
    isCompact: state.isCompact,
    openMobile: state.openMobile,
    setOpenMobile: state.setOpenMobile,
  }),
}));
vi.mock("./app-rail", () => ({ AppRail: () => <nav data-testid="rail" /> }));
vi.mock("./space-panel", () => ({
  SpacePanelContent: ({ space }: { space: string | null }) => <div data-testid={`panel-${space}`} />,
}));
vi.mock("./app-shell-context", () => ({
  useAppShell: () => ({ space: "work", hasPanel: true }),
}));

beforeEach(() => {
  state.pathname = "/acme/issues";
  state.isCompact = true;
  state.openMobile = false;
  state.setOpenMobile.mockClear();
});

// The sheet covers the page on a phone: a tap that navigates but leaves it
// open renders the destination underneath and reads as a dead tap.
describe("CompactNavSheet dismissal", () => {
  it("dismisses the sheet once the route changes", () => {
    const { rerender } = renderWithI18n(<CompactNavSheet />);
    state.setOpenMobile.mockClear();

    state.pathname = "/acme/inbox";
    rerender(<CompactNavSheet />);
    expect(state.setOpenMobile).toHaveBeenCalledWith(false);
  });

  it("does not re-dismiss while the route holds still", () => {
    const { rerender } = renderWithI18n(<CompactNavSheet />);
    state.setOpenMobile.mockClear();

    rerender(<CompactNavSheet />);
    expect(state.setOpenMobile).not.toHaveBeenCalled();
  });
});

describe("CompactNavSheet content", () => {
  it("holds the rail and the current space's panel", () => {
    state.openMobile = true;
    const { getByTestId } = renderWithI18n(<CompactNavSheet />);
    expect(getByTestId("rail")).toBeInTheDocument();
    expect(getByTestId("panel-work")).toBeInTheDocument();
  });

  it("renders nothing above the compact breakpoint", () => {
    state.isCompact = false;
    state.openMobile = true;
    const { queryByTestId } = renderWithI18n(<CompactNavSheet />);
    expect(queryByTestId("rail")).toBeNull();
  });
});
