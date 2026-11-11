import { act, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Pencil } from "@multica/ui/icons";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

describe("ObjectProperty label", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  const tooltip = () => document.querySelector("[data-slot=tooltip-content]");

  function hover(el: HTMLElement) {
    vi.useFakeTimers();
    fireEvent.pointerEnter(el, { pointerType: "mouse" });
    act(() => {
      vi.advanceTimersByTime(550);
    });
  }

  // jsdom does not lay out text: model a translation wider than the label column.
  function setWidths(el: HTMLElement, scrollWidth: number) {
    el.style.overflowX = "hidden";
    Object.defineProperties(el, {
      scrollWidth: { configurable: true, value: scrollWidth },
      clientWidth: { configurable: true, value: 80 },
    });
  }

  it("shows the full label on hover when it is cut off", () => {
    renderWithI18n(<ObjectProperty label="Identifiant du daemon">a1b2c3</ObjectProperty>);
    const label = screen.getByText("Identifiant du daemon");
    setWidths(label, 124);
    hover(label);
    expect(tooltip()).toHaveTextContent("Identifiant du daemon");
  });

  it("stays quiet when the label fits", () => {
    renderWithI18n(<ObjectProperty label="Runtime">MacBook Pro</ObjectProperty>);
    const label = screen.getByText("Runtime");
    setWidths(label, 48);
    hover(label);
    expect(tooltip()).toBeNull();
  });

  describe("keyboard", () => {
    // The label re-measures when it resizes; jsdom never fires that, so the
    // test does once it has given the labels their widths.
    let resized: (() => void)[] = [];
    beforeEach(() => {
      resized = [];
      vi.stubGlobal(
        "ResizeObserver",
        class {
          constructor(private callback: () => void) {}
          observe() {
            resized.push(() => this.callback());
          }
          disconnect() {}
        },
      );
    });
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    function renderPanel() {
      renderWithI18n(
        <>
          <button type="button">Before</button>
          <ObjectProperty label="Ejecuciones (7 d)">
            <button type="button">Runs</button>
          </ObjectProperty>
          <ObjectProperty label="Estado">
            <button type="button">Status</button>
          </ObjectProperty>
        </>,
      );
      setWidths(screen.getByText("Ejecuciones (7 d)"), 98);
      setWidths(screen.getByText("Estado"), 38);
      act(() => resized.forEach((fire) => fire()));
    }

    it("reaches a cut-off label with Tab and shows its full text until focus leaves", async () => {
      const user = userEvent.setup();
      renderPanel();
      const label = screen.getByText("Ejecuciones (7 d)");
      screen.getByRole("button", { name: "Before" }).focus();

      await user.tab();
      expect(label).toHaveFocus();
      expect(tooltip()).toHaveTextContent("Ejecuciones (7 d)");

      await user.tab();
      expect(screen.getByRole("button", { name: "Runs" })).toHaveFocus();
      expect(tooltip()).toBeNull();
    });

    it("keeps a label that fits out of the tab order", async () => {
      const user = userEvent.setup();
      renderPanel();
      screen.getByRole("button", { name: "Runs" }).focus();

      await user.tab();
      expect(screen.getByRole("button", { name: "Status" })).toHaveFocus();
      expect(screen.getByText("Estado")).not.toHaveAttribute("tabindex");
      expect(tooltip()).toBeNull();
    });
  });
});
