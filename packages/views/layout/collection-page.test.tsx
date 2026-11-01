import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Users } from "@multica/ui/icons";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import {
  CollectionCreateButton,
  CollectionFilterButton,
  CollectionPageHeader,
  CollectionPageState,
  CollectionScopeItems,
} from "./collection-page";

describe("CollectionPageHeader", () => {
  it("renders the scope as the title with the tools beside it and icon actions on the right", () => {
    renderWithI18n(
      <CollectionPageHeader
        icon={Users}
        title="All teams"
        tools={<CollectionFilterButton />}
        actions={<CollectionCreateButton label="New team" onClick={vi.fn()} />}
      />,
    );

    const header = screen.getByRole("banner");
    expect(within(header).getByRole("heading", { name: "All teams", level: 1 })).toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "Filter" })).toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "New team" })).toBeInTheDocument();
  });

  it("opens the scope menu from the title and reports the picked scope", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(
      <CollectionPageHeader
        icon={Users}
        title="My teams"
        menu={
          <CollectionScopeItems
            value="mine"
            onChange={onChange}
            options={[
              { value: "mine", label: "My teams", count: 2 },
              { value: "all", label: "All teams", count: 5 },
            ]}
          />
        }
      />,
    );

    await user.click(screen.getByRole("button", { name: /My teams/ }));
    const all = await screen.findByRole("menuitemradio", { name: /All teams/ });
    expect(all).toHaveTextContent("5");
    await user.click(all);
    expect(onChange).toHaveBeenCalledWith("all");
  });
});

describe("CollectionFilterButton", () => {
  it("names the number of filters that are on", () => {
    renderWithI18n(<CollectionFilterButton count={2} />);
    const button = screen.getByRole("button", { name: "2 filters" });
    expect(button).toHaveTextContent("2");
  });
});

describe("CollectionPageState", () => {
  it("renders an alert with semantic title and description", () => {
    renderWithI18n(
      <CollectionPageState
        role="alert"
        icon={Users}
        title="Could not load teams"
        description="Try again."
      />,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Could not load teams", level: 2 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Try again.").tagName).toBe("P");
  });
});
