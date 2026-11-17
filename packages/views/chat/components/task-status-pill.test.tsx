import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatPendingTask } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import { TaskStatusPill } from "./task-status-pill";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

function pill(pendingTask: ChatPendingTask) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <TaskStatusPill
        pendingTask={pendingTask}
        taskMessages={[]}
        availability={undefined}
      />
    </I18nProvider>
  );
}

afterEach(() => {
  vi.useRealTimers();
});

describe("TaskStatusPill elapsed anchor", () => {
  it("restarts the timer when a new task begins instead of inheriting the previous task's baseline", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-24T12:00:30.000Z"));

    // Task A started 30s ago.
    const { rerender, container } = render(
      pill({ task_id: "task-a", status: "running", created_at: "2026-06-24T12:00:00.000Z" }),
    );
    expect(container.textContent).toContain("· 30s");

    // Sending a new message starts task B "now" — the pill must restart at 0s,
    // not keep counting from task A's 30s baseline (#4264).
    rerender(
      pill({ task_id: "task-b", status: "running", created_at: "2026-06-24T12:00:30.000Z" }),
    );
    expect(container.textContent).toContain("· 0s");
    expect(container.textContent).not.toContain("· 30s");
  });

  it("keeps the anchor stable while the same task's created_at is refined (no backward snap)", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-24T12:00:30.000Z"));

    // Optimistic anchor seeded to ~now.
    const { rerender, container } = render(
      pill({ task_id: "task-a", status: "running", created_at: "2026-06-24T12:00:30.000Z" }),
    );
    expect(container.textContent).toContain("· 0s");

    // Same task, server created_at refined a few seconds earlier — the anchor
    // stays locked within a task so the timer never snaps backward/forward.
    rerender(
      pill({ task_id: "task-a", status: "running", created_at: "2026-06-24T12:00:25.000Z" }),
    );
    expect(container.textContent).toContain("· 0s");
    expect(container.textContent).not.toContain("· 5s");
  });
});
