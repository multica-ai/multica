import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IssueSidebarCollapsible, IssueSidebarPropertyGrid, IssueSidebarSectionHeader } from "./issue-sidebar-section";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("issue sidebar disclosure", () => {
  it("keeps an empty title static while its add action remains available", () => {
    const toggle = vi.fn();
    const add = vi.fn();
    render(<IssueSidebarSectionHeader title="References" count={0} open onOpenChange={toggle} collapsible={false}
      actions={<button onClick={add}>Add reference</button>} />);
    fireEvent.click(screen.getByText("References"));
    expect(toggle).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "References" })).not.toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
    expect(screen.getByText("References").nextElementSibling).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add reference" }));
    expect(add).toHaveBeenCalledOnce();
  });

  it("keeps section actions independent and supports keyboard disclosure", async () => {
    const add = vi.fn();
    function Example() {
      const [open, setOpen] = useState(true);
      return <>
        <IssueSidebarSectionHeader title="References" count={2} open={open} onOpenChange={setOpen}
          actions={<button onClick={add}>Add reference</button>} />
        {open && <p>Linked item</p>}
      </>;
    }
    render(<Example />);
    const disclosure = screen.getByRole("button", { name: "References" });
    // The disclosure arrow trails the title text instead of leading it.
    expect(screen.getByText("References").nextElementSibling).toHaveAttribute("aria-hidden", "true");
    fireEvent.click(screen.getByRole("button", { name: "Add reference" }));
    expect(add).toHaveBeenCalledOnce();
    expect(disclosure).toHaveAttribute("aria-expanded", "true");
    disclosure.focus();
    await userEvent.keyboard(" ");
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
    expect(disclosure).toHaveAccessibleName("References 2");
    expect(screen.queryByText("Linked item")).not.toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByText("Linked item")).toBeVisible();
  });

  it("counts an open section only when its list is cut short", () => {
    const { rerender } = render(<IssueSidebarSectionHeader title="Runs" count={9} open onOpenChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Runs" })).toBeInTheDocument();
    rerender(<IssueSidebarSectionHeader title="Runs" count={9} countWhenOpen open onOpenChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Runs 9" })).toBeInTheDocument();
    rerender(<IssueSidebarSectionHeader title="Runs" count={9} open={false} onOpenChange={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Runs 9" })).toBeInTheDocument();
  });

  it("sizes the property label column by the labels it is given, not the rows present", () => {
    const { container } = render(
      <IssueSidebarPropertyGrid sizerLabels={["Status", "Fecha de entrega"]}>
        <div className="col-span-2">row</div>
      </IssueSidebarPropertyGrid>,
    );
    const sizer = container.querySelector("[aria-hidden='true']");
    expect([...sizer!.children].map((label) => label.getAttribute("data-label"))).toEqual(["Status", "Fecha de entrega"]);
    expect(sizer!.textContent).toBe("");
    expect(sizer).toHaveClass("invisible", "col-start-1", "h-0");
  });

  it("toggles IssueSidebarCollapsible content and honors reduced motion", () => {
    vi.stubGlobal("matchMedia", vi.fn(() => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })));
    function CollapsibleExample() {
      const [open, setOpen] = useState(true);
      return <>
        <IssueSidebarSectionHeader title="Details" open={open} onOpenChange={setOpen} />
        <IssueSidebarCollapsible open={open}>
          <p>Detail row</p>
        </IssueSidebarCollapsible>
      </>;
    }
    const { container } = render(<CollapsibleExample />);
    expect(screen.getByText("Detail row")).toBeVisible();
    const panel = container.querySelector('[data-slot="collapsible-content"]');
    expect(panel?.className).toContain("transition-none");
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.queryByText("Detail row")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByText("Detail row")).toBeVisible();
  });
});
