// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import type { AgentTask } from "@multica/core/types/agent";
import { useTranscriptViewStore } from "@multica/core/agents/stores";
import { renderWithI18n } from "../../test/i18n";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import type { TimelineItem } from "./build-timeline";

vi.mock("@multica/core/api", () => ({
  api: {
    getAgent: vi.fn().mockResolvedValue(null),
    listRuntimes: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("../actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? <>{children}</> : null,
  DialogContent: ({ children }: { children: ReactNode }) => (
    <div role="dialog">{children}</div>
  ),
  DialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({
    children,
    ...props
  }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuCheckboxItem: ({
    checked,
    onCheckedChange,
    children,
  }: {
    checked?: boolean;
    onCheckedChange?: (checked: boolean) => void;
    children: ReactNode;
  }) => (
    <button
      type="button"
      role="menuitemcheckbox"
      aria-checked={checked === true}
      onClick={() => onCheckedChange?.(checked !== true)}
    >
      {children}
    </button>
  ),
  DropdownMenuItem: ({
    children,
    onClick,
    className: _className,
  }: ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/collapsible", async () => {
  const React = await import("react");
  const Context = React.createContext<{
    open: boolean;
    onOpenChange?: (open: boolean) => void;
  }>({ open: false });

  return {
    Collapsible: ({
      open,
      onOpenChange,
      children,
    }: {
      open: boolean;
      onOpenChange?: (open: boolean) => void;
      children: ReactNode;
    }) => (
      <Context.Provider value={{ open, onOpenChange }}>{children}</Context.Provider>
    ),
    CollapsibleTrigger: ({
      disabled,
      children,
      className: _className,
    }: ButtonHTMLAttributes<HTMLButtonElement>) => {
      const ctx = React.useContext(Context);
      return (
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            if (!disabled) ctx.onOpenChange?.(!ctx.open);
          }}
        >
          {children}
        </button>
      );
    },
    CollapsibleContent: ({ children }: { children: ReactNode }) => {
      const ctx = React.useContext(Context);
      return ctx.open ? <div>{children}</div> : null;
    },
  };
});

const baseTask: AgentTask = {
  id: "task-1",
  agent_id: "",
  runtime_id: "",
  issue_id: "issue-1",
  status: "completed",
  priority: 0,
  dispatched_at: null,
  started_at: "2026-06-08T08:00:00Z",
  completed_at: "2026-06-08T08:01:00Z",
  result: null,
  error: null,
  created_at: "2026-06-08T08:00:00Z",
};

const items: TimelineItem[] = [
  {
    seq: 1,
    type: "text",
    content: "Agent summary\nAgent hidden detail",
  },
  {
    seq: 2,
    type: "thinking",
    content: "Thinking summary\nThinking hidden detail",
  },
  {
    seq: 3,
    type: "tool_use",
    tool: "terminal",
    input: { command: "pnpm test" },
  },
];

function renderDialog(dialogItems: TimelineItem[] = items) {
  return renderWithI18n(
    <AgentTranscriptDialog
      open
      onOpenChange={vi.fn()}
      task={baseTask}
      items={dialogItems}
      agentName="Codex"
    />,
  );
}

beforeEach(() => {
  cleanup();
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    preserveFilters: false,
    selectedFilterKeys: [],
    defaultExpanded: false,
  });
});

afterEach(() => {
  cleanup();
});

describe("AgentTranscriptDialog", () => {
  it("preserves selected filters across dialog remounts when enabled", () => {
    const first = renderDialog();

    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Thinking" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Preserve filters" }));

    expect(screen.queryByText("Agent summary")).not.toBeInTheDocument();
    expect(screen.getByText(/Thinking summary/)).toBeInTheDocument();
    expect(useTranscriptViewStore.getState().selectedFilterKeys).toEqual(["thinking"]);

    first.unmount();
    renderDialog();

    expect(screen.queryByText("Agent summary")).not.toBeInTheDocument();
    expect(screen.getByText(/Thinking summary/)).toBeInTheDocument();
  });

  it("ignores stale persisted filter keys that are not available in the current transcript", () => {
    useTranscriptViewStore.setState({
      preserveFilters: true,
      selectedFilterKeys: ["thinking"],
    });

    renderDialog([
      {
        seq: 1,
        type: "text",
        content: "Only agent summary\nOnly agent hidden detail",
      },
    ]);

    expect(screen.getByText("Only agent summary")).toBeInTheDocument();
    expect(screen.queryByText("No execution data recorded.")).not.toBeInTheDocument();
  });

  it("expands and collapses every currently visible detailed row", () => {
    renderDialog();

    expect(screen.queryByText(/Agent hidden detail/)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command": "pnpm test"/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand visible" }));

    expect(screen.getByText(/Agent hidden detail/)).toBeInTheDocument();
    expect(screen.getByText(/"command": "pnpm test"/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Collapse visible" }));

    expect(screen.queryByText(/Agent hidden detail/)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command": "pnpm test"/)).not.toBeInTheDocument();
  });

  it("uses the default-expanded preference for newly opened transcripts", () => {
    useTranscriptViewStore.setState({ defaultExpanded: true });

    renderDialog();

    expect(screen.getByText(/Agent hidden detail/)).toBeInTheDocument();
    expect(screen.getByText(/"command": "pnpm test"/)).toBeInTheDocument();
  });
});
