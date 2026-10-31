import { screen, within } from "@testing-library/react";
import { Pencil } from "@multica/ui/icons";
import { describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import {
  ObjectIdentity,
  ObjectPage,
  ObjectProperty,
  ObjectSection,
  ObjectSectionAction,
} from "./object-page";

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/a1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
}

function renderPage(panel?: React.ReactNode) {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <ObjectPage
        layoutId="test_object_page"
        segments={[{ href: "/acme/agents", label: "Agents" }]}
        leaf={<span>Lambda</span>}
        actions={<button type="button">Assign work</button>}
        panel={panel}
      >
        <ObjectIdentity media={<span />} title="Lambda" description="Design and frontend" />
        <ObjectSection
          title="Instructions"
          action={<ObjectSectionAction icon={Pencil}>Edit</ObjectSectionAction>}
        >
          <p>Mock before you build.</p>
        </ObjectSection>
      </ObjectPage>
    </NavigationProvider>,
  );
}

describe("ObjectPage", () => {
  it("puts the list it came from in the breadcrumb and the actions in the header", () => {
    renderPage();
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Agents" })).toHaveAttribute("href", "/acme/agents");
    expect(within(header).getByText("Lambda")).toBeInTheDocument();
    expect(within(header).getByRole("button", { name: "Assign work" })).toBeInTheDocument();
  });

  it("carries a section's actions on the section title, not in the header", () => {
    renderPage();
    const section = screen.getByRole("heading", { name: "Instructions", level: 2 }).closest("section")!;
    expect(within(section).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    expect(within(screen.getByRole("banner")).queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("adds the panel toggle only when the page has a panel", () => {
    renderPage();
    expect(screen.queryByRole("button", { name: "Toggle right sidebar" })).toBeNull();
  });

  it("renders the properties panel beside the body", () => {
    renderPage(<ObjectProperty label="Runtime">MacBook Pro</ObjectProperty>);
    expect(screen.getByRole("button", { name: "Toggle right sidebar" })).toBeInTheDocument();
    expect(screen.getByText("Runtime")).toBeInTheDocument();
    expect(screen.getByText("MacBook Pro")).toBeInTheDocument();
  });
});
