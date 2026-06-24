// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { FilterCondition } from "../layout";

// The filter builder mounts dropdown / popover / command primitives for the
// "+ Condition" and "Project" pickers. The is/is-not toggle under test is a
// plain button in the chip, so we stub the heavy primitives to passthroughs.
// Factories are hoisted, so the stub helper is defined inside each one.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => <div>{children}</div>;
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger: ({ children }: { children?: React.ReactNode }) => <button>{children}</button>,
    DropdownMenuContent: Pass,
    DropdownMenuItem: Pass,
    DropdownMenuLabel: Pass,
    DropdownMenuGroup: Pass,
    DropdownMenuSeparator: Pass,
  };
});
vi.mock("@multica/ui/components/ui/popover", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => <div>{children}</div>;
  return {
    Popover: Pass,
    PopoverContent: Pass,
    PopoverTrigger: ({ children }: { children?: React.ReactNode }) => <button>{children}</button>,
  };
});
vi.mock("@multica/ui/components/ui/command", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => <div>{children}</div>;
  return {
    Command: Pass,
    CommandInput: () => <input />,
    CommandList: Pass,
    CommandEmpty: Pass,
    CommandGroup: Pass,
    CommandItem: Pass,
  };
});

import { FilterBuilder } from "./filter-builder";

afterEach(() => cleanup());

describe("FilterBuilder negation toggle (FIR-1731)", () => {
  it("flips a condition between is and is not", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const filters: FilterCondition[] = [{ field: "unread" }];
    render(<FilterBuilder filters={filters} projects={[]} onChange={onChange} />);

    // Starts as "is" (include).
    const toggle = screen.getByRole("button", { name: "Set condition to exclude" });
    expect(toggle.textContent).toBe("is");

    await user.click(toggle);
    expect(onChange).toHaveBeenCalledWith([{ field: "unread", negate: true }]);
  });

  it("renders an already-negated condition as is not", () => {
    render(
      <FilterBuilder
        filters={[{ field: "unread", negate: true }]}
        projects={[]}
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Set condition to include" }).textContent,
    ).toBe("is not");
  });
});
