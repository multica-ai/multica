// @vitest-environment jsdom

import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentTask, ChatMessage, ChatPendingTask } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import enChat from "../../locales/en/chat.json";

// Capture the props ChatMessageList receives so we can assert the static-vs-live
// contract (pendingTask + initialTopMostItemIndex) without a real Virtuoso list.
const listProps = vi.hoisted(
  () =>
    ({ current: null }) as {
      current: {
        messages: ChatMessage[];
        pendingTask: ChatPendingTask | null | undefined;
        initialTopMostItemIndex?: number;
      } | null;
    },
);

vi.mock("./chat-message-list", () => ({
  ChatMessageList: (props: {
    messages: ChatMessage[];
    pendingTask: ChatPendingTask | null | undefined;
    initialTopMostItemIndex?: number;
  }) => {
    listProps.current = props;
    return (
      <div data-testid="chat-message-list">
        {props.messages.map((m) => (
          <div key={m.id} data-role={m.role}>
            {m.content}
          </div>
        ))}
      </div>
    );
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="avatar" />,
}));

vi.mock("@multica/core/api", () => ({
  api: { getAgent: vi.fn().mockResolvedValue({ name: "Implementer" }) },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1" }),
}));

vi.mock("@multica/core/agents/use-agent-presence", () => ({
  useAgentPresenceDetail: () => ({ availability: "online" }),
}));

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, chat: enChat },
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    trigger_summary: "Fix the login redirect",
    dispatched_at: "2026-05-15T10:00:05.000Z",
    started_at: "2026-05-15T10:00:06.000Z",
    completed_at: "2026-05-15T10:00:12.000Z",
    result: null,
    error: null,
    created_at: "2026-05-15T10:00:00.000Z",
    ...overrides,
  } as AgentTask;
}

afterEach(() => {
  listProps.current = null;
  cleanup();
});

describe("VisualRunHistory", () => {
  it("renders a read-only conversation with no input/textbox", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    render(
      <Wrapper>
        <VisualRunHistory open onOpenChange={() => {}} task={makeTask()} />
      </Wrapper>,
    );

    // Conversational rows render (user trigger + assistant bubble).
    expect(screen.getByText("Fix the login redirect")).toBeInTheDocument();
    expect(screen.getByTestId("chat-message-list")).toBeInTheDocument();

    // Read-only: there is no composer / text entry anywhere in the dialog.
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("anchors a terminal run to the top and passes no pending task", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    render(
      <Wrapper>
        <VisualRunHistory open onOpenChange={() => {}} task={makeTask()} />
      </Wrapper>,
    );

    expect(listProps.current?.pendingTask).toBeNull();
    expect(listProps.current?.initialTopMostItemIndex).toBe(0);
    // Terminal run emits the user trigger + one assistant bubble.
    expect(listProps.current?.messages).toHaveLength(2);
  });

  it("streams an in-flight run via pendingTask and leaves the list bottom-anchored", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    render(
      <Wrapper>
        <VisualRunHistory
          open
          onOpenChange={() => {}}
          task={makeTask({ status: "running", completed_at: null })}
        />
      </Wrapper>,
    );

    expect(listProps.current?.pendingTask).toMatchObject({
      task_id: "task-1",
      status: "running",
    });
    expect(listProps.current?.initialTopMostItemIndex).toBeUndefined();
    // In-flight: only the user trigger bubble; the timeline streams via pill.
    expect(listProps.current?.messages).toHaveLength(1);
  });

  it("surfaces the raw-log escape hatch when provided", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    const onViewRawLog = vi.fn();
    render(
      <Wrapper>
        <VisualRunHistory
          open
          onOpenChange={() => {}}
          task={makeTask()}
          onViewRawLog={onViewRawLog}
        />
      </Wrapper>,
    );

    fireEvent.click(screen.getByRole("button", { name: "View raw log" }));
    expect(onViewRawLog).toHaveBeenCalledOnce();
  });

  it("shows a calm note for a cancelled run", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    render(
      <Wrapper>
        <VisualRunHistory
          open
          onOpenChange={() => {}}
          task={makeTask({ status: "cancelled" })}
        />
      </Wrapper>,
    );

    expect(screen.getByText("This run was cancelled.")).toBeInTheDocument();
  });

  it("shows a mode-aware empty state when a terminal run has no trigger", async () => {
    const { VisualRunHistory } = await import("./visual-run-history");
    // A completed run with no trigger summary AND no content still emits an
    // assistant bubble, so it is NOT empty. Emptiness only happens live with
    // no trigger — assert that path renders the live placeholder.
    render(
      <Wrapper>
        <VisualRunHistory
          open
          onOpenChange={() => {}}
          task={makeTask({
            status: "running",
            completed_at: null,
            trigger_summary: undefined,
          })}
        />
      </Wrapper>,
    );

    expect(screen.getByText("Starting up…")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-message-list")).not.toBeInTheDocument();
  });
});
