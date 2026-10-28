import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

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
    const disclosure = screen.getByRole("button", { name: "References 2" });
    // The disclosure arrow trails the title text instead of leading it.
    expect(screen.getByText("References").nextElementSibling).toHaveAttribute("aria-hidden", "true");
    fireEvent.click(screen.getByRole("button", { name: "Add reference" }));
    expect(add).toHaveBeenCalledOnce();
    expect(disclosure).toHaveAttribute("aria-expanded", "true");
    disclosure.focus();
    await userEvent.keyboard(" ");
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Linked item")).not.toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByText("Linked item")).toBeVisible();
  });
});
