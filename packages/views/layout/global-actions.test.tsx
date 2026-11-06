import type { ReactNode } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { NewIssueButton, SearchButton } from "./global-actions";

const state = vi.hoisted(() => ({ pathname: "/acme/inbox", hasDraft: false }));
const openCreateIssueWithPreference = vi.hoisted(() => vi.fn());
const setSearchOpen = vi.hoisted(() => vi.fn());

vi.mock("../navigation", () => ({
  useNavigation: () => ({ pathname: state.pathname }),
}));
vi.mock("@multica/core/issues/stores", () => ({ openCreateIssueWithPreference }));
vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: (selector: (s: { hasDraft: () => boolean }) => unknown) =>
    selector({ hasDraft: () => state.hasDraft }),
}));
vi.mock("../search/search-store", () => ({
  useSearchStore: { getState: () => ({ setOpen: setSearchOpen }) },
}));
vi.mock("@multica/core/shortcuts", () => ({ useShortcut: () => null }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

beforeEach(() => {
  state.pathname = "/acme/inbox";
  state.hasDraft = false;
  openCreateIssueWithPreference.mockClear();
  setSearchOpen.mockClear();
});

describe("NewIssueButton", () => {
  it("starts an empty issue outside a project", () => {
    renderWithI18n(<NewIssueButton className="" tooltipSide="bottom" />);
    fireEvent.click(screen.getByRole("button", { name: /^new issue$/i }));
    expect(openCreateIssueWithPreference).toHaveBeenCalledWith(undefined);
  });

  it("files the issue into the open project, like the C shortcut", () => {
    state.pathname = "/acme/projects/p-1";
    renderWithI18n(<NewIssueButton className="" tooltipSide="bottom" />);
    fireEvent.click(screen.getByRole("button", { name: /^new issue$/i }));
    expect(openCreateIssueWithPreference).toHaveBeenCalledWith({ project_id: "p-1" });
  });

  it("marks a saved draft", () => {
    state.hasDraft = true;
    renderWithI18n(<NewIssueButton className="" tooltipSide="bottom" />);
    expect(screen.getByRole("button", { name: /^new issue$/i }).querySelector(".bg-brand")).not.toBeNull();
  });
});

describe("SearchButton", () => {
  it("opens search", () => {
    renderWithI18n(<SearchButton className="" tooltipSide="bottom" />);
    fireEvent.click(screen.getByRole("button", { name: "Search..." }));
    expect(setSearchOpen).toHaveBeenCalledWith(true);
  });
});
