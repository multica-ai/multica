import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatWakeup } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import enCommon from "../../locales/en/common.json";
import enInbox from "../../locales/en/inbox.json";

const reschedule = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async (original) => ({
  ...(await original<typeof import("@multica/core/api")>()),
  api: { rescheduleChatWakeup: reschedule },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("sonner", () => ({ toast: { error: toastError } }));

import { ChatWakeupHeaderButton } from "./chat-wakeups";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const NOW = new Date(2026, 10, 24, 10, 10).getTime();

function check(overrides: Partial<ChatWakeup> = {}): ChatWakeup {
  return {
    id: "check-1",
    chat_session_id: "session-1",
    agent_id: "agent-1",
    title: "Watch the staging deploy",
    instruction: "Report the deploy result",
    kind: "check",
    condition: null,
    interval_seconds: 1800,
    fire_at: null,
    next_eval_at: new Date(NOW + 30 * MINUTE).toISOString(),
    expires_at: new Date(NOW + 23 * HOUR + 50 * MINUTE).toISOString(),
    status: "active",
    end_reason: null,
    ended_at: null,
    check_count: 6,
    last_check_note: null,
    last_checked_at: null,
    last_task_id: null,
    last_fired_at: null,
    last_error: null,
    source_message_id: null,
    source_task_id: "task-1",
    created_at: new Date(NOW - 10 * MINUTE).toISOString(),
    updated_at: new Date(NOW).toISOString(),
    ...overrides,
  };
}

function renderPanel(wakeups: ChatWakeup[]) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { chat: enChat, common: enCommon, inbox: enInbox } }}>
        <ChatWakeupHeaderButton sessionId="session-1" wakeups={wakeups} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Wakeups" }));
  return { invalidate };
}

describe("Wakeup timing editor", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(NOW);
    reschedule.mockReset();
    toastError.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it("moves the deadline earlier when a faster interval would pass the check limit, and saves both", async () => {
    reschedule.mockResolvedValue(undefined);
    const { invalidate } = renderPanel([check()]);

    fireEvent.click(await screen.findByRole("button", { name: "Change timing" }));
    expect(screen.getByRole("button", { name: "30 minutes" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "No change" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "10 minutes" }));
    // 23h50m of 10-minute checks is 143; the deadline ends at the 100th instead.
    expect(screen.queryByRole("button", { name: "No change" })).toBeNull();
    expect(screen.getByRole("button", { name: /tomorrow/i })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save" })).toBeNull());
    expect(reschedule).toHaveBeenCalledWith("session-1", "check-1", {
      interval_seconds: 600,
      expires_at: new Date(NOW + 100 * 10 * MINUTE).toISOString(),
    });
    expect(invalidate).toHaveBeenCalled();
  });

  it("keeps the editor open and says so when the change fails", async () => {
    reschedule.mockRejectedValue(new Error("offline"));
    renderPanel([check({ kind: "condition", interval_seconds: null, condition: { type: "issue" } })]);

    fireEvent.click(await screen.findByRole("button", { name: "Change timing" }));
    expect(screen.queryByRole("group", { name: "Every" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "in 4 hours" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Couldn't change the timing"));
    expect(reschedule).toHaveBeenCalledWith("session-1", "check-1", {
      expires_at: new Date(NOW + 4 * HOUR).toISOString(),
    });
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("offers later reminder times, not only earlier ones", async () => {
    reschedule.mockResolvedValue(undefined);
    renderPanel([
      check({
        kind: "at",
        interval_seconds: null,
        fire_at: new Date(NOW + 2 * HOUR).toISOString(),
        expires_at: new Date(NOW + 2 * HOUR).toISOString(),
      }),
    ]);

    fireEvent.click(await screen.findByRole("button", { name: "Change timing" }));
    fireEvent.click(screen.getByRole("button", { name: /tomorrow/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(reschedule).toHaveBeenCalledWith("session-1", "check-1", {
        at: new Date(2026, 10, 25, 9).toISOString(),
      }),
    );
  });
});
