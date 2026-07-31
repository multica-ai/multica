// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";

const mockState = vi.hoisted(() => ({
  continueTaskInChat: vi.fn(),
  push: vi.fn(),
}));

vi.mock("@multica/core/api", async () => {
  // ApiError and dispatchReasonCode are real logic the component branches on,
  // so mocking them would test the mock. Only the network call is replaced.
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: { continueTaskInChat: mockState.continueTaskInChat },
  };
});

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/acme/chat" }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockState.push }),
}));

const toasts = vi.hoisted(() => ({ error: vi.fn(), warning: vi.fn() }));
vi.mock("sonner", () => ({ toast: toasts }));

import { ApiError } from "@multica/core/api";
import {
  ContinueInChatButton,
  canContinueTaskInChat,
} from "./continue-in-chat-button";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: "2026-06-08T08:10:00Z",
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  };
}

function result(overrides: Record<string, unknown> = {}) {
  return {
    chat_session: { id: "session-9" },
    reopened: false,
    session_carried: true,
    work_dir_carried: true,
    ...overrides,
  };
}

beforeEach(() => {
  mockState.continueTaskInChat.mockReset();
  mockState.push.mockReset();
  toasts.error.mockReset();
  toasts.warning.mockReset();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("canContinueTaskInChat", () => {
  // The gate exists because a live task still owns its provider session and its
  // work_dir, and a reused work_dir has no mutual exclusion — offering the button
  // there would invite two runs into one directory.
  //
  // Typed as a total map over AgentTask["status"] rather than a pair of string
  // arrays: adding a status to that union then fails to compile here until it is
  // classified, instead of silently defaulting to "not continuable". The DB CHECK
  // also permits 'deferred', which this union does not carry — that wider domain
  // is enumerated in the Go test (TestIsTerminalTaskStatus), which is where the
  // authoritative status list lives.
  it("admits only terminal statuses", () => {
    const expected: Record<NonNullable<AgentTask["status"]>, boolean> = {
      completed: true,
      failed: true,
      cancelled: true,
      queued: false,
      dispatched: false,
      running: false,
      waiting_local_directory: false,
    };
    for (const [status, want] of Object.entries(expected)) {
      expect(
        canContinueTaskInChat(
          makeTask({ status: status as AgentTask["status"] }),
        ),
      ).toBe(want);
    }
  });
});

describe("ContinueInChatButton", () => {
  it("navigates to the created chat session", async () => {
    mockState.continueTaskInChat.mockResolvedValue(result());
    renderWithI18n(<ContinueInChatButton task={makeTask()} />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() => {
      expect(mockState.push).toHaveBeenCalledWith("/acme/chat?session=session-9");
    });
    expect(mockState.continueTaskInChat).toHaveBeenCalledWith("task-1");
    expect(toasts.warning).not.toHaveBeenCalled();
  });

  // The honesty requirement: when the run left no resumable session the chat
  // still opens, but the user must be told the agent starts without the context —
  // silently landing them in a fresh conversation that looks continuous is the
  // worst outcome this feature can produce.
  it("warns but still navigates when no session was carried", async () => {
    mockState.continueTaskInChat.mockResolvedValue(
      result({ session_carried: false }),
    );
    renderWithI18n(<ContinueInChatButton task={makeTask()} />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() => {
      expect(mockState.push).toHaveBeenCalledWith("/acme/chat?session=session-9");
    });
    expect(toasts.warning).toHaveBeenCalledTimes(1);
    expect(String(toasts.warning.mock.calls[0]?.[0])).toMatch(/resumable session/i);
  });

  it("localizes a permission block instead of echoing the server message", async () => {
    mockState.continueTaskInChat.mockRejectedValue(
      new ApiError("you don't have permission to use this target", 403, "Forbidden", {
        error: "you don't have permission to use this target",
        reason_code: "invocation_not_allowed",
      }),
    );
    renderWithI18n(<ContinueInChatButton task={makeTask()} />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() => expect(toasts.error).toHaveBeenCalledTimes(1));
    expect(String(toasts.error.mock.calls[0]?.[0])).toMatch(/permission/i);
    expect(mockState.push).not.toHaveBeenCalled();
  });

  // Reachable when the run goes live again between render and click.
  it("explains a non-terminal refusal from the structured reason", async () => {
    mockState.continueTaskInChat.mockRejectedValue(
      new ApiError("still running", 409, "Conflict", {
        error: "the task is still running; stop it before continuing in chat",
        reason: "task_not_terminal",
        task_status: "running",
      }),
    );
    renderWithI18n(<ContinueInChatButton task={makeTask()} />);

    fireEvent.click(screen.getByRole("button"));

    await waitFor(() => expect(toasts.error).toHaveBeenCalledTimes(1));
    expect(String(toasts.error.mock.calls[0]?.[0])).toMatch(/still going|stop it/i);
    expect(mockState.push).not.toHaveBeenCalled();
  });

  it("re-enables the button after a failure so the user can retry", async () => {
    mockState.continueTaskInChat.mockRejectedValue(new Error("network down"));
    renderWithI18n(<ContinueInChatButton task={makeTask()} />);

    const button = screen.getByRole("button");
    fireEvent.click(button);

    await waitFor(() => expect(toasts.error).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(button).not.toBeDisabled());
  });
});
