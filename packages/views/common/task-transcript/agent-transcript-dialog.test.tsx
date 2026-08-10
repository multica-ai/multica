// @vitest-environment jsdom

import { readFileSync } from "node:fs";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { api } from "@multica/core/api";
import type { AgentRuntime, AgentTask } from "@multica/core/types/agent";
import { useTranscriptViewStore } from "@multica/core/agents/stores";
import { renderWithI18n } from "../../test/i18n";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import type { TimelineItem } from "./build-timeline";

const copyTextMock = vi.hoisted(() => vi.fn().mockResolvedValue(true));

vi.mock("@multica/core/api", () => ({
  api: {
    getAgent: vi.fn().mockResolvedValue(null),
    listRuntimes: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: copyTextMock,
}));

// Real react-virtuoso renders no data rows under jsdom's zero-height viewport,
// so stub it with a flat render to make rows visible to these tests.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
  }: {
    data: TimelineItem[];
    itemContent: (i: number, item: TimelineItem) => ReactNode;
    computeItemKey: (i: number, item: TimelineItem) => number;
  }) => (
    <div>
      {data.map((item, i) => (
        <div key={computeItemKey(i, item)}>{itemContent(i, item)}</div>
      ))}
    </div>
  ),
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

vi.mock("@multica/ui/components/ui/dropdown-menu", async () => {
  const React = await import("react");
  const RadioContext = React.createContext<{
    value?: string;
    onValueChange?: (value: string) => void;
  }>({});

  return {
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
  DropdownMenuRadioGroup: ({
    value,
    onValueChange,
    children,
  }: {
    value?: string;
    onValueChange?: (value: string) => void;
    children: ReactNode;
  }) => (
    <RadioContext.Provider value={{ value, onValueChange }}>{children}</RadioContext.Provider>
  ),
  DropdownMenuRadioItem: ({
    value,
    children,
  }: {
    value: string;
    children: ReactNode;
  }) => {
    const ctx = React.useContext(RadioContext);
    return (
      <button
        type="button"
        role="menuitemradio"
        aria-checked={ctx.value === value}
        onClick={() => ctx.onValueChange?.(value)}
      >
        {children}
      </button>
    );
  },
  };
});

// The transcript body renders agent markdown through RichContent; stub it to
// keep these tests independent of the markdown pipeline.
vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => (
    <div data-testid="rich-content">{content}</div>
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
      ...props
    }: ButtonHTMLAttributes<HTMLButtonElement>) => {
      const ctx = React.useContext(Context);
      return (
        <button
          type="button"
          disabled={disabled}
          aria-expanded={ctx.open}
          onClick={() => {
            if (!disabled) ctx.onOpenChange?.(!ctx.open);
          }}
          {...props}
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

const liveTask: AgentTask = {
  ...baseTask,
  runtime_id: "runtime-1",
  status: "running",
  completed_at: null,
};

function runtimeFor(provider: string): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "workspace-1",
    daemon_id: "daemon-1",
    name: `${provider} runtime`,
    runtime_mode: "local",
    provider,
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "owner-1",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-06-08T08:00:00Z",
    updated_at: "2026-06-08T08:00:00Z",
  };
}

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

function renderDialog(
  dialogItems: TimelineItem[] = items,
  options: { task?: AgentTask; isLive?: boolean } = {},
) {
  return renderWithI18n(
    <AgentTranscriptDialog
      open
      onOpenChange={vi.fn()}
      task={options.task ?? baseTask}
      items={dialogItems}
      agentName="Codex"
      isLive={options.isLive}
    />,
  );
}

beforeEach(() => {
  cleanup();
  copyTextMock.mockClear();
  vi.mocked(api.listRuntimes).mockResolvedValue([]);
  useTranscriptViewStore.setState({
    sortDirection: "chronological",
    selectedFilterKeys: [],
    // Legacy row assertions below expect one-line summaries; smart density is
    // exercised by its own tests.
    density: "collapsed",
  });
});

afterEach(() => {
  cleanup();
});

describe("AgentTranscriptDialog", () => {
  it("explains unavailable live events for an empty Antigravity transcript", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFor("antigravity")]);

    renderDialog([], { task: liveTask, isLive: true });

    expect(
      await screen.findByText(
        "Antigravity does not currently provide live execution events. The transcript will be available after the task completes.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Waiting for events...")).not.toBeInTheDocument();
  });

  it("keeps waiting for live events from other runtimes", async () => {
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFor("hermes")]);

    renderDialog([], { task: liveTask, isLive: true });

    // Runtime detail now lives in the ⓘ popover; its trigger appearing proves
    // the runtime loaded. The non-antigravity live state still waits.
    await screen.findByRole("button", { name: "Run details" });
    expect(screen.getByText("Waiting for events...")).toBeInTheDocument();
  });

  it("preserves selected filters across dialog remounts unconditionally", () => {
    const first = renderDialog();

    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: "Thinking" }));

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

  it("switches wholesale between expand-all and collapse-all via the density menu", () => {
    renderDialog();

    expect(screen.queryByText(/Agent hidden detail/)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command": "pnpm test"/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitemradio", { name: /Expand all/ }));

    expect(screen.getByText(/Agent hidden detail/)).toBeInTheDocument();
    expect(screen.getByText(/Thinking hidden detail/)).toBeInTheDocument();
    expect(screen.getByText(/"command": "pnpm test"/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitemradio", { name: /Collapse all/ }));

    expect(screen.queryByText(/Agent hidden detail/)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command": "pnpm test"/)).not.toBeInTheDocument();
  });

  it("smart density opens agent text in place and keeps process noise folded", () => {
    useTranscriptViewStore.setState({ density: "smart" });

    renderDialog();

    // Agent body reads without a click (through RichContent), tools stay folded.
    expect(screen.getByTestId("rich-content")).toHaveTextContent("Agent hidden detail");
    expect(screen.queryByText(/Thinking hidden detail/)).not.toBeInTheDocument();
    expect(screen.queryByText(/"command": "pnpm test"/)).not.toBeInTheDocument();
  });

  it("row-level toggles override the density default until the mode changes", () => {
    useTranscriptViewStore.setState({ density: "smart" });

    renderDialog();

    // Fold the default-open agent body back to one line. The `expanded`
    // filter distinguishes the collapse trigger from the timeline segment,
    // which also carries the "Agent" accessible name via its title.
    fireEvent.click(screen.getByRole("button", { name: "Agent", expanded: true }));
    expect(screen.queryByTestId("rich-content")).not.toBeInTheDocument();
    expect(screen.getByText("Agent summary")).toBeInTheDocument();

    // Open a default-folded thinking row.
    fireEvent.click(screen.getByRole("button", { name: /Thinking summary/ }));
    expect(screen.getByText(/Thinking hidden detail/)).toBeInTheDocument();
  });

  it("copies RFC 3339 timestamps before event labels", () => {
    renderDialog([
      {
        seq: 1,
        type: "text",
        content: "Agent summary\nAgent hidden detail",
        created_at: "2026-06-08T08:00:00+08:00",
      },
      {
        seq: 2,
        type: "thinking",
        content: "Thinking summary",
        created_at: "2026-06-08T08:00:05.123Z",
      },
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Copy all" }));

    // Full body (not the truncated summary) with the RFC 3339 prefix, events
    // separated by a blank line.
    expect(copyTextMock).toHaveBeenCalledWith(
      [
        "[2026-06-08T00:00:00.000Z] [Agent] Agent summary\nAgent hidden detail",
        "[2026-06-08T08:00:05.123Z] [Thinking] Thinking summary",
      ].join("\n\n"),
    );
  });

  it("keeps older events without a valid timestamp copyable", () => {
    renderDialog([
      {
        seq: 1,
        type: "text",
        content: "Missing timestamp",
      },
      {
        seq: 2,
        type: "error",
        content: "Invalid timestamp",
        created_at: "not-a-date",
      },
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Copy all" }));

    expect(copyTextMock).toHaveBeenCalledWith(
      ["[Agent] Missing timestamp", "[Error] Invalid timestamp"].join("\n\n"),
    );
  });
  it("renders a file edit as a diff instead of escaped JSON strings", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    const { container } = renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: {
          file_path: "/repo/src/counter.rs",
          old_string: "pub count: u32,",
          new_string: "pub count: u32,\npub total: u64,",
        },
      },
    ]);

    // Changed lines read as diff rows. Text is asserted on the row, not on a
    // leaf node: syntax highlighting splits a line across `hljs-*` spans.
    const rowText = (selector: string) =>
      Array.from(container.querySelectorAll(selector)).map((el) => el.textContent?.trim());
    // The first line is unchanged, so it is context; only the second is added.
    expect(rowText(".bg-success\\/10")).toEqual(["+ pub total: u64,"]);
    expect(rowText(".bg-destructive\\/10")).toEqual([]);
    expect(container.querySelector("pre")?.textContent).toContain("pub count: u32,");
    // ...and the raw JSON keys are gone from the surface.
    expect(screen.queryByText(/"new_string"/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\\n/)).not.toBeInTheDocument();
  });

  it("highlights diff rows using the grammar for the file extension", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    const { container } = renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: {
          file_path: "/repo/src/lib.rs",
          old_string: "let a = 1;",
          new_string: "let b = 2;",
        },
      },
    ]);

    // `let` is a Rust keyword, so the highlighter must have marked it up.
    expect(container.querySelector(".hljs-keyword")?.textContent).toBe("let");
  });

  it("carries the scope class the hljs palette is defined under", () => {
    // The palette lives in editor/styles/code.css, scoped to the editor surface
    // and this class. Without it the spans render but stay uncoloured.
    useTranscriptViewStore.setState({ density: "expanded" });
    const { container } = renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: { file_path: "/repo/src/lib.rs", old_string: "let a = 1;", new_string: "let b = 2;" },
      },
    ]);

    expect(container.querySelector("pre")?.className).toContain("transcript-code");
    const css = readFileSync("editor/styles/code.css", "utf8");
    expect(css).toContain(".transcript-code");
  });

  it("leaves an unknown extension unhighlighted rather than guessing", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    const { container } = renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: { file_path: "/repo/NOTES", old_string: "let a = 1;", new_string: "let b = 2;" },
      },
    ]);

    expect(container.querySelector(".hljs-keyword")).toBeNull();
    expect(container.textContent).toContain("let b = 2;");
  });

  it("unwraps a JSON-encoded tool result so it reads as terminal output", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    renderDialog([
      {
        seq: 1,
        type: "tool_result",
        tool: "Bash",
        output: '"total 0\\ndrwxr-xr-x  2 user  staff"',
      },
    ]);

    expect(
      screen.getByText("total 0\ndrwxr-xr-x  2 user  staff", {
        selector: "pre",
        // Keep the newline and column spacing the unwrap is meant to restore.
        normalizer: (text) => text,
      }),
    ).toBeInTheDocument();
  });
  it("shows a whole-file write as plain content with a line count", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    const { container } = renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Write",
        input: { file_path: "/f.rs", content: "alpha\nbeta" },
      },
    ]);

    expect(screen.getByText("+2")).toBeInTheDocument();
    // Plain content: no per-line + gutter and no diff tinting.
    const pre = container.querySelector("pre");
    expect(pre?.textContent).toBe("alpha\nbeta");
    expect(container.querySelector(".bg-success\\/10")).toBeNull();
  });

  it("clamps a long body behind the show-all affordance", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    const content = Array.from({ length: 40 }, (_, i) => `line ${i}`).join("\n");
    const { container } = renderDialog([
      { seq: 1, type: "tool_use", tool: "Write", input: { file_path: "/f.rs", content } },
    ]);

    const pre = container.querySelector("pre");
    expect(pre?.className).toContain("max-h-52");
    expect(pre?.className).toContain("overflow-hidden");
    expect(screen.getByRole("button", { name: "Show all" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show all" }));
    expect(container.querySelector("pre")?.className).not.toContain("max-h-52");
  });

  it("does not clamp a short diff", () => {
    useTranscriptViewStore.setState({ density: "expanded" });
    renderDialog([
      {
        seq: 1,
        type: "tool_use",
        tool: "Edit",
        input: { file_path: "/f.rs", old_string: "a", new_string: "b" },
      },
    ]);

    expect(screen.queryByRole("button", { name: "Show all" })).not.toBeInTheDocument();
  });

  // The run-details provider row used to carry its own name map. Pointing it at
  // the shared runtime formatter is right for every provider except Claude,
  // whose runtime-list name is "Claude" while this diagnostic row has always
  // said "Claude Code" — and whose legacy `claude-code` slug title-cases into
  // "Claude-code". Both aliases are pinned here so the next cleanup keeps them.
  it.each([
    ["claude", "Claude Code"],
    ["claude-code", "Claude Code"],
    // Everything outside the alias table defers to the shared runtime
    // formatter, so a new provider needs no edit here.
    ["omp", "Oh-My-Pi"],
  ])("names a %s run %s in the run details", async (provider, expected) => {
    const user = userEvent.setup();
    vi.mocked(api.listRuntimes).mockResolvedValue([runtimeFor(provider)]);

    renderDialog(items, { task: { ...baseTask, runtime_id: "runtime-1" } });

    await user.click(await screen.findByRole("button", { name: "Run details" }));
    expect(await screen.findByText(expected)).toBeInTheDocument();
  });
});
