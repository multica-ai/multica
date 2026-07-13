// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { FilterCondition } from "../layout";

// The filter builder mounts dropdown / popover / command primitives for the
// "+ Condition" / "Project" pickers AND for the is/is-not and Match all/any
// selectors (FIR-1731). We stub the heavy primitives to passthroughs; the
// DropdownMenuItem stub forwards onClick so the menu choices stay testable.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => <div>{children}</div>;
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger: ({ children }: { children?: React.ReactNode }) => <button>{children}</button>,
    DropdownMenuContent: Pass,
    DropdownMenuItem: ({ children, onClick }: { children?: React.ReactNode; onClick?: () => void }) => (
      <button onClick={onClick}>{children}</button>
    ),
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

describe("FilterBuilder is/is not selector (FIR-1731)", () => {
  it("flips an include condition to exclude from the menu", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const filters: FilterCondition[] = [{ field: "unread" }];
    render(
      <FilterBuilder filters={filters} projects={[]} onChange={onChange} onMatchChange={vi.fn()} />,
    );

    await user.click(screen.getByRole("button", { name: /is not — exclude matching/ }));
    expect(onChange).toHaveBeenCalledWith([{ field: "unread", negate: true }]);
  });

  it("flips an exclude condition back to include from the menu", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <FilterBuilder
        filters={[{ field: "unread", negate: true }]}
        projects={[]}
        onChange={onChange}
        onMatchChange={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: /is — include matching/ }));
    expect(onChange).toHaveBeenCalledWith([{ field: "unread", negate: false }]);
  });
});

describe("FilterBuilder Match all / Match any (FIR-1731)", () => {
  it("hides the AND/OR toggle with fewer than two conditions", () => {
    render(
      <FilterBuilder
        filters={[{ field: "unread" }]}
        projects={[]}
        onChange={vi.fn()}
        onMatchChange={vi.fn()}
      />,
    );
    expect(screen.queryByText(/Match any — at least one/)).toBeNull();
  });

  it("switches the combine mode to any (OR) from the menu", async () => {
    const user = userEvent.setup();
    const onMatchChange = vi.fn();
    render(
      <FilterBuilder
        filters={[{ field: "unread" }, { field: "pinned" }]}
        projects={[]}
        onChange={vi.fn()}
        match="all"
        onMatchChange={onMatchChange}
      />,
    );

    await user.click(screen.getByRole("button", { name: /Match any — at least one/ }));
    expect(onMatchChange).toHaveBeenCalledWith("any");
  });
});
