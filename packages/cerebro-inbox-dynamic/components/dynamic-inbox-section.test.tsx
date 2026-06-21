// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import type { Project } from "@multica/core/types";
import type { SectionFilterContext, DynInboxEntry } from "../section-filter";
import type { InboxSectionConfig } from "../layout";
import { DynamicInboxSection, runStateFor } from "./dynamic-inbox-section";

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

// FIR-394 — DynamicInboxSection reads cerebro_inbox_hide_reminders via
// useFeatureFlag, which calls useWorkspaceId and throws when rendered bare.
// Default off here so these tests keep their pre-flag behavior (reminders shown).
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => false,
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

describe("runStateFor — scheduled wakeup dot (FIR-1521)", () => {
  function ctxWith(over: Partial<InboxActionContext>): SectionFilterContext {
    return {
      action: {
        userId: "me",
        issueRunStates: new Map(),
        subIssueRunStates: new Map(),
        chatRunStates: new Map(),
        mentionedChannels: new Set(),
        wakeupIssueIds: new Set(),
        ...over,
      } as InboxActionContext,
      matchesPins: () => false,
    } as SectionFilterContext;
  }
  const notifEntry = (issueId: string) =>
    ({ kind: "notif", id: "n1", time: 0, item: { id: "n1", issue_id: issueId } } as DynInboxEntry);

  it("returns 'scheduled' for an idle issue with a pending wakeup", () => {
    expect(runStateFor(notifEntry("iss1"), ctxWith({ wakeupIssueIds: new Set(["iss1"]) }))).toBe(
      "scheduled",
    );
  });

  it("prefers a live run over the scheduled dot when both apply", () => {
    expect(
      runStateFor(
        notifEntry("iss1"),
        ctxWith({ issueRunStates: new Map([["iss1", "active"]]), wakeupIssueIds: new Set(["iss1"]) }),
      ),
    ).toBe("active");
  });

  it("returns undefined when the issue has neither a run nor a wakeup", () => {
    expect(runStateFor(notifEntry("iss1"), ctxWith({}))).toBeUndefined();
  });
});
