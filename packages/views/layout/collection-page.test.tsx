import { render, screen } from "@testing-library/react";
import { Plus, Users } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "./collection-page";

describe("CollectionPageHeader", () => {
  it("renders a semantic title, count, supporting link, and labelled action", () => {
    render(
      <CollectionPageHeader
        icon={Users}
        title="Teams"
        count={4}
        description="Manage collaborators."
        learnMore={{ href: "https://example.com/docs", label: "Learn more" }}
        actions={
          <CollectionPageHeaderAction
            icon={Plus}
            label="New team"
            onClick={vi.fn()}
          />
        }
      />,
    );

    expect(screen.getByRole("banner")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Teams", level: 1 }),
    ).toBeInTheDocument();
    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Learn more" })).toHaveAttribute(
      "href",
      "https://example.com/docs",
    );
    expect(screen.getByRole("button", { name: "New team" })).toBeInTheDocument();
  });

  it("does not render a zero count", () => {
    render(<CollectionPageHeader icon={Users} title="Teams" count={0} />);
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });
});

describe("CollectionPageState", () => {
  it("renders an alert with semantic title and description", () => {
    render(
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
