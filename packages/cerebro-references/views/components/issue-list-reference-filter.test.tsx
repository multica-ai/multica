import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "@multica/views/navigation";
import { IssueListReferenceFilter } from "./issue-list-reference-filter";

function renderFilter(searchParams = new URLSearchParams()) {
  const push = vi.fn();
  render(
    <NavigationProvider
      value={{
        push,
        replace: vi.fn(),
        back: vi.fn(),
        pathname: "/acme/issues",
        searchParams,
        openInNewTab: vi.fn(),
        getShareableUrl: (path) => `https://app.test${path}`,
      }}
    >
      <IssueListReferenceFilter />
    </NavigationProvider>,
  );
  return { push };
}

describe("IssueListReferenceFilter", () => {
  it("writes a normalized github_pr reference into the URL", () => {
    const { push } = renderFilter();

    fireEvent.click(screen.getByRole("button", { name: /reference/i }));
    fireEvent.change(screen.getByLabelText("Reference"), {
      target: { value: "https://github.com/firtal-group/firtal-cerebro/pull/525" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    expect(push).toHaveBeenCalledWith(
      "/acme/issues?reference=github_pr%3Afirtal-group%2Ffirtal-cerebro%23525",
    );
  }, 30_000);

  it("clears the reference query parameter without dropping other params", () => {
    const params = new URLSearchParams({
      reference: "github_pr:firtal-group/firtal-cerebro#525",
      view: "list",
    });
    const { push } = renderFilter(params);

    fireEvent.click(screen.getByRole("button", {
      name: /GitHub PR firtal-group\/firtal-cerebro#525/i,
    }));
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));

    expect(push).toHaveBeenCalledWith("/acme/issues?view=list");
  }, 30_000);
});
