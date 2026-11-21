import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatWakeup } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ChatWakeupStrip, useChatWakeups } from "./chat-wakeups";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({
  api: {
    cancelChatWakeup: vi.fn(),
    listChatSessionWakeups: vi.fn(async () => []),
    listMyChatWakeups: vi.fn(async () => []),
  },
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const HOUR = 3_600_000;

function wakeup(id: string, expiresInHours: number): ChatWakeup {
  const now = Date.now();
  return {
    id,
    chat_session_id: "session-1",
    agent_id: "agent-1",
    title: id,
    instruction: "Report back",
    kind: "condition",
    condition: { type: "pull_request" },
    interval_seconds: null,
    fire_at: null,
    next_eval_at: new Date(now + 30_000).toISOString(),
    expires_at: new Date(now + expiresInHours * HOUR).toISOString(),
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
  };
}

function clientWith(wakeups: ChatWakeup[]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  qc.setQueryData(chatKeys.wakeups("session-1"), wakeups);
  return qc;
}

/** Reads the same cache the cancel mutation patches, as the chat page does. */
function Strip() {
  const wakeups = useChatWakeups("session-1");
  return <ChatWakeupStrip sessionId="session-1" wakeups={wakeups} />;
}

function renderStrip(wakeups: ChatWakeup[]) {
  return renderWithI18n(
    <QueryClientProvider client={clientWith(wakeups)}>
      <Strip />
    </QueryClientProvider>,
  );
}

describe("cancelling a wakeup", () => {
  afterEach(() => vi.clearAllMocks());

  it("still reports a failure after the only active wakeup unmounts the strip", async () => {
    let reject!: (err: unknown) => void;
    vi.mocked(api.cancelChatWakeup).mockImplementation(
      () => new Promise((_resolve, rej) => { reject = rej; }),
    );
    renderStrip([wakeup("only", 20)]);

    fireEvent.click(screen.getByRole("button", { name: "Cancel wakeup" }));
    await waitFor(() => expect(screen.queryByText("Waiting: only")).not.toBeInTheDocument());

    await act(async () => reject(new Error("cancel failed")));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Couldn't cancel the wakeup"));
  });

  it("still reports a failure after the popover row moves to the ended list", async () => {
    let reject!: (err: unknown) => void;
    vi.mocked(api.cancelChatWakeup).mockImplementation(
      () => new Promise((_resolve, rej) => { reject = rej; }),
    );
    renderStrip([wakeup("sooner", 20), wakeup("later", 40)]);

    fireEvent.click(screen.getByRole("button", { name: "Wakeups" }));
    const cancels = screen.getAllByRole("button", { name: "Cancel wakeup" });
    // The strip button is first. The next one belongs to the row that leaves the active list.
    fireEvent.click(cancels[1]!);
    await waitFor(() => expect(api.cancelChatWakeup).toHaveBeenCalledWith("session-1", "sooner"));

    await act(async () => reject(new Error("cancel failed")));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Couldn't cancel the wakeup"));
  });
});
