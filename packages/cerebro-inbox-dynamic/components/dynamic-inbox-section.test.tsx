// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import type { Project } from "@multica/core/types";
import type { SectionFilterContext } from "../section-filter";
import type { InboxSectionConfig } from "../layout";
import { DynamicInboxSection } from "./dynamic-inbox-section";

afterEach(() => cleanup());

vi.mock("@multica/ui/components/ui/dropdown-menu", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/ui/components/ui/dropdown-menu")>();
  return {
    ...actual,
    DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => render,
    DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuItem: ({
      children,
      onClick,
    }: {
      children: React.ReactNode;
      onClick?: () => void;
    }) => (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
    DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuSeparator: () => <hr />,
  };
});

vi.mock("./dynamic-inbox-row", () => ({
  DynamicInboxRow: () => null,
}));

const filterContext: SectionFilterContext = {
  action: {
    userId: "me",
    issueRunStates: new Map(),
    subIssueRunStates: new Map(),
    chatRunStates: new Map(),
    mentionedChannels: new Set(),
    wakeupIssueIds: new Set(),
  } satisfies InboxActionContext,
  matchesPins: () => false,
};

function renderSection(section: Partial<InboxSectionConfig> = {}) {
  const onChange = vi.fn();
  const props: React.ComponentProps<typeof DynamicInboxSection> = {
    section: {
      id: "s1",
      kind: "all",
      countStyle: "circle",
      ...section,
    },
    entries: [],
    filterContext,
    actionLabels: {
      act_now: "Act now",
      reminders: "Reminders",
      watching: "Watching",
      pending: "Pending",
      waiting: "Waiting",
      calm: "Calm",
    },
    projects: [] as Project[],
    selectedKey: null,
    onSelect: vi.fn(),
    onArchive: vi.fn(),
    onChange,
    onRemove: vi.fn(),
    onMove: vi.fn(),
    isFirst: true,
    isLast: true,
  };

  render(<DynamicInboxSection {...props} />);
  return { onChange };
}

describe("DynamicInboxSection badge color", () => {
  it("renders the circle count with the selected semantic color", () => {
    renderSection({ badgeColor: "success" });

    expect(screen.getByText("0").className).toContain("bg-success/10");
    expect(screen.getByText("0").className).toContain("text-success");
  });

  it("persists the selected badge color from section settings", async () => {
    const user = userEvent.setup();
    const { onChange } = renderSection({ badgeColor: "brand" });

    await user.click(screen.getByRole("button", { name: /warning/i }));

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ id: "s1", countStyle: "circle", badgeColor: "warning" }),
    );
  });
});
