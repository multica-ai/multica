import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { describe, expect, it, vi } from "vitest";
import type { ChatMessage, ChatWakeup } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";

const cancelMutate = vi.hoisted(() => vi.fn());
const retryMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/chat/mutations", () => ({
  useCancelChatWakeup: () => ({ mutate: cancelMutate }),
  useRetryChatWakeup: () => ({ mutate: retryMutate, isPending: false }),
}));

import { ChatQueue } from "./chat-queue";
import {
  ChatWakeupCards,
  ChatWakeupDivider,
  ChatWakeupStrip,
  isWakeupRunningOut,
  wakeupReportState,
} from "./chat-wakeups";

const TEST_RESOURCES = { en: { chat: enChat } };
const HOUR = 3_600_000;

function wakeup(overrides: Partial<ChatWakeup> & Pick<ChatWakeup, "id">): ChatWakeup {
  const now = Date.now();
  return {
    chat_session_id: "session-1",
    agent_id: "agent-1",
    title: "PR #42 merged",
    instruction: "Tell the user what changed",
    kind: "condition",
    condition: { type: "pull_request" },
    interval_seconds: null,
    fire_at: null,
    next_eval_at: new Date(now + 30_000).toISOString(),
    expires_at: new Date(now + 20 * HOUR).toISOString(),
    status: "active",
    end_reason: null,
    ended_at: null,
    check_count: 0,
    last_check_note: null,
    last_checked_at: null,
    last_task_id: null,
    last_fired_at: null,
    last_error: null,
    source_message_id: null,
    source_task_id: "task-1",
    created_at: new Date(now - 4 * HOUR).toISOString(),
    updated_at: new Date(now).toISOString(),
    ...overrides,
  };
}

function renderWithI18n(node: React.ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {node}
    </I18nProvider>,
  );
}

describe("isWakeupRunningOut", () => {
  const now = Date.now();

  it("flags the last 30 minutes and the last fifth of the window", () => {
    expect(isWakeupRunningOut(wakeup({ id: "a" }), now)).toBe(false);
    expect(
      isWakeupRunningOut(wakeup({ id: "b", expires_at: new Date(now + 20 * 60_000).toISOString() }), now),
    ).toBe(true);
    expect(
      isWakeupRunningOut(
        wakeup({
          id: "c",
          created_at: new Date(now - 90 * HOUR).toISOString(),
          expires_at: new Date(now + 10 * HOUR).toISOString(),
        }),
        now,
      ),
    ).toBe(true);
  });

  it("never flags reminders or ended wakeups", () => {
    const soon = new Date(now + 60_000).toISOString();
    expect(isWakeupRunningOut(wakeup({ id: "a", kind: "at", expires_at: soon }), now)).toBe(false);
    expect(isWakeupRunningOut(wakeup({ id: "b", status: "fired", expires_at: soon }), now)).toBe(false);
  });
});

describe("ChatWakeupStrip", () => {
  it("shows the wait that ends first, counts the rest, and cancels it", () => {
    const now = Date.now();
    renderWithI18n(
      <ChatWakeupStrip
        sessionId="session-1"
        wakeups={[
          wakeup({ id: "later", title: "Deploy finished", expires_at: new Date(now + 40 * HOUR).toISOString() }),
          wakeup({ id: "sooner", title: "PR #42 merged" }),
          wakeup({ id: "done", title: "Old wait", status: "fired" }),
        ]}
      />,
    );

    expect(screen.getByText("Waiting: PR #42 merged")).toBeInTheDocument();
    expect(screen.getByText("+1")).toBeInTheDocument();
    expect(screen.getByText(/ends in 20 hours/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Cancel wakeup" }));
    expect(cancelMutate).toHaveBeenCalledWith(
      { sessionId: "session-1", wakeupId: "sooner" },
      expect.anything(),
    );
  });

  it("renders nothing without an active wakeup", () => {
    const { container } = renderWithI18n(
      <ChatWakeupStrip sessionId="session-1" wakeups={[wakeup({ id: "a", status: "cancelled" })]} />,
    );
    expect(container.querySelector('[data-slot="chat-wakeup-strip"]')).toBeNull();
  });
});

describe("ChatWakeupDivider", () => {
  const message: ChatMessage = {
    id: "m-1",
    chat_session_id: "session-1",
    role: "user",
    content: "",
    task_id: "task-9",
    created_at: new Date().toISOString(),
    message_kind: "wakeup",
    wakeup_id: "w-1",
  };

  it("says why the agent came back", () => {
    renderWithI18n(<ChatWakeupDivider message={message} wakeup={wakeup({ id: "w-1", status: "fired" })} />);
    expect(screen.getByText("Back to report · it happened · PR #42 merged")).toBeInTheDocument();
  });

  it("marks the turn the deadline started", () => {
    renderWithI18n(
      <ChatWakeupDivider
        message={message}
        wakeup={wakeup({ id: "w-1", status: "expired", last_task_id: "task-9" })}
      />,
    );
    expect(screen.getByText("Back to report · deadline reached · PR #42 merged")).toBeInTheDocument();
  });

  it("falls back when the wakeup is no longer listed", () => {
    renderWithI18n(<ChatWakeupDivider message={message} wakeup={undefined} />);
    expect(screen.getByText("Back to report")).toBeInTheDocument();
  });
});

describe("ChatWakeupCards", () => {
  it("shows the promise and its state under the reply", () => {
    renderWithI18n(
      <ChatWakeupCards
        wakeups={[
          wakeup({ id: "a", title: "PR #42 merged" }),
          wakeup({ id: "b", title: "Ask Alice", status: "cancelled" }),
        ]}
      />,
    );
    expect(screen.getByText("Will report back: PR #42 merged")).toBeInTheDocument();
    expect(screen.getByText(/ends in 20 hours/)).toBeInTheDocument();
    expect(screen.getByText("Cancelled")).toBeInTheDocument();
  });

  it("says reported back only once the report turn finished, and offers a retry when it failed", () => {
    const fired = (id: string, last_task_status?: string) =>
      wakeup({ id, title: id, status: "fired", last_task_id: `task-${id}`, last_task_status });
    renderWithI18n(
      <ChatWakeupCards
        wakeups={[fired("queued", "queued"), fired("running", "running"), fired("done", "completed"), fired("broken", "failed")]}
      />,
    );
    expect(screen.getByText("Waiting to report")).toBeInTheDocument();
    expect(screen.getByText("Reporting...")).toBeInTheDocument();
    expect(screen.getByText("Reported back")).toBeInTheDocument();
    expect(screen.getByText("Couldn't report")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retryMutate).toHaveBeenCalledWith({ sessionId: "session-1", wakeupId: "broken" }, expect.anything());
  });
});

describe("wakeupReportState", () => {
  it("follows the report turn of a wakeup that came back", () => {
    const base = { id: "w", status: "fired" as const, last_task_id: "t" };
    expect(wakeupReportState(wakeup({ ...base, last_task_status: "dispatched" }))).toBe("waiting");
    expect(wakeupReportState(wakeup({ ...base, last_task_status: "running" }))).toBe("running");
    expect(wakeupReportState(wakeup({ ...base, last_task_status: "cancelled" }))).toBe("failed");
    expect(wakeupReportState(wakeup({ ...base, last_task_status: "completed" }))).toBeNull();
    expect(wakeupReportState(wakeup({ id: "w", status: "active", last_task_id: "t", last_task_status: "failed" }))).toBeNull();
    // finish runs inside the check's turn, before its reply is written.
    const finished = { id: "w", kind: "check" as const, status: "fired" as const, last_task_id: "t" };
    expect(wakeupReportState(wakeup({ ...finished, last_task_status: "running" }))).toBe("running");
    expect(wakeupReportState(wakeup({ ...finished, last_task_status: "failed" }))).toBe("failed");
  });
});

describe("ChatQueue wakeup turns", () => {
  it("names a queued wakeup turn by its wakeup instead of the hidden input", () => {
    renderWithI18n(
      <ChatQueue
        headStatus="running"
        tasks={[
          { task_id: "t-1", status: "queued", created_at: "", wakeup_id: "w-1" },
          { task_id: "t-2", status: "queued", created_at: "", wakeup_id: "w-gone" },
        ]}
        wakeups={[wakeup({ id: "w-1", title: "PR #42 merged" })]}
        onSendNow={vi.fn()}
        onEdit={vi.fn()}
        onRemove={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByText("Wakeup: PR #42 merged")).toBeInTheDocument();
    expect(screen.getByText("Wakeup")).toBeInTheDocument();
  });
});
