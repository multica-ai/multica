import { useState } from "react";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Users } from "@multica/ui/icons";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import {
  CollectionCreateButton,
  CollectionFilterButton,
  CollectionNoMatches,
  CollectionPageHeader,
  CollectionPageState,
  CollectionScopeItems,
  CollectionSearch,
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

describe("CollectionNoMatches", () => {
  it("names the reset after what is narrowing the list", async () => {
    const user = userEvent.setup();
    const onReset = vi.fn();
    const { rerender } = renderWithI18n(
      <CollectionNoMatches title="No teams match" onReset={onReset} hasFilters hasSearch />,
    );
    await user.click(screen.getByRole("button", { name: "Clear filters and search" }));
    expect(onReset).toHaveBeenCalledOnce();

    rerender(<CollectionNoMatches title="No teams match" onReset={onReset} hasSearch />);
    expect(screen.getByRole("button", { name: "Clear search" })).toBeInTheDocument();

    rerender(<CollectionNoMatches title="No teams match" onReset={onReset} hasFilters />);
    expect(screen.getByRole("button", { name: "Clear filters" })).toBeInTheDocument();
  });

  it("offers no reset when nothing is narrowing the list", () => {
    renderWithI18n(<CollectionNoMatches title="No teams match" onReset={vi.fn()} />);
    expect(screen.getByRole("heading", { name: "No teams match" })).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});

function SearchHarness({ initial = "" }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <CollectionSearch value={value} onChange={setValue} label="Search teams" />
      <input aria-label="Other field" />
      <button type="button">Elsewhere</button>
    </>
  );
}

/** jsdom lays nothing out; pretend the header is on screen. */
function showPage() {
  vi.spyOn(HTMLElement.prototype, "getClientRects").mockReturnValue(
    [{}] as unknown as DOMRectList,
  );
}

describe("CollectionSearch", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("starts as an icon and opens a focused field on click", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);
    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Search teams" }));

    const field = screen.getByRole("textbox", { name: "Search teams" });
    expect(field).toHaveFocus();
    expect(field).toHaveAttribute("placeholder", "Search teams");
  });

  it("closes an empty field when focus leaves and keeps a field with a query open", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);

    await user.click(screen.getByRole("button", { name: "Search teams" }));
    await user.click(screen.getByRole("button", { name: "Elsewhere" }));
    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Search teams" }));
    await user.keyboard("ops");
    await user.click(screen.getByRole("button", { name: "Elsewhere" }));
    expect(screen.getByRole("textbox", { name: "Search teams" })).toHaveValue("ops");
  });

  it("empties and closes on Escape", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);

    await user.click(screen.getByRole("button", { name: "Search teams" }));
    await user.keyboard("ops{Escape}");

    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Search teams" }));
    expect(screen.getByRole("textbox", { name: "Search teams" })).toHaveValue("");
  });

  it("empties and closes from the clear button, handing focus back to the icon", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);

    await user.click(screen.getByRole("button", { name: "Search teams" }));
    await user.keyboard("ops");
    await user.click(screen.getByRole("button", { name: "Clear search" }));

    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search teams" })).toHaveFocus();
  });

  it("shows a query restored from the store without taking focus", () => {
    renderWithI18n(<SearchHarness initial="ops" />);

    const field = screen.getByRole("textbox", { name: "Search teams" });
    expect(field).toHaveValue("ops");
    expect(field).not.toHaveFocus();
  });

  it("opens from \"/\" on the page without typing the slash", async () => {
    showPage();
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);

    await user.keyboard("/");

    const field = screen.getByRole("textbox", { name: "Search teams" });
    expect(field).toHaveFocus();
    expect(field).toHaveValue("");
  });

  it("leaves \"/\" alone in another field, with a modifier, or on a hidden page", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SearchHarness />);

    // Hidden page: nothing laid out.
    await user.keyboard("/");
    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();

    showPage();
    await user.keyboard("{Control>}/{/Control}");
    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();

    const other = screen.getByRole("textbox", { name: "Other field" });
    await user.click(other);
    await user.keyboard("/");
    expect(other).toHaveValue("/");
    expect(screen.queryByRole("textbox", { name: "Search teams" })).not.toBeInTheDocument();
  });
});
