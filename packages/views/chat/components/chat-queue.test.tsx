import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { describe, expect, it, vi } from "vitest";
import enChat from "../../locales/en/chat.json";
import { ChatQueue } from "./chat-queue";

const TEST_RESOURCES = { en: { chat: enChat } };

function renderQueue(headStatus = "running") {
  const callbacks = {
    onSendNow: vi.fn<(taskId: string) => Promise<void>>().mockResolvedValue(),
    onEdit: vi.fn<(taskId: string) => Promise<void>>().mockResolvedValue(),
    onRemove: vi.fn<(taskId: string) => Promise<void>>().mockResolvedValue(),
    onClear: vi.fn<() => Promise<void>>().mockResolvedValue(),
  };
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatQueue
        headStatus={headStatus}
        tasks={[
          {
            task_id: "task-2",
            status: "queued",
            content: "First follow-up",
            created_at: "2026-07-01T00:01:00Z",
          },
          {
            task_id: "task-3",
            status: "queued",
            content: "",
            created_at: "2026-07-01T00:02:00Z",
          },
        ]}
        {...callbacks}
      />
    </I18nProvider>,
  );
  return { ...callbacks, container: view.container };
}

describe("ChatQueue", () => {
  it("renders a standalone queue card without a separate header", () => {
    const { container } = renderQueue();

    expect(screen.getByRole("region", { name: "2 queued messages" })).toBeInTheDocument();
    expect(screen.queryByText("2 queued messages")).not.toBeInTheDocument();
    expect(screen.getByText("First follow-up")).toBeInTheDocument();
    expect(screen.getByText("Queued message")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Steer" })).toHaveLength(2);
    expect(screen.getAllByLabelText("Remove queued message")).toHaveLength(2);
    expect(screen.getAllByLabelText("More queue actions")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Clear all" })).not.toBeInTheDocument();

    const shell = container.querySelector('[data-slot="chat-queue-shell"]');
    const queue = container.querySelector('[data-slot="chat-queue"]');
    // Tucked stack, owned entirely by the queue: it slides under the composer
    // (negative margin + z-0) while the composer's chrome stays untouched.
    expect(shell).toHaveClass("z-0", "-mb-3");
    expect(queue).toHaveClass(
      "rounded-lg",
      "border-surface-border",
      "bg-surface",
      "pb-4",
    );
    expect(container.querySelectorAll('[data-slot="chat-queue-row"]')).toHaveLength(2);
    expect(container.querySelectorAll('[data-slot="chat-queue-item-icon"]')).toHaveLength(2);
  });

  it("runs steer, remove, and overflow actions against the selected queue state", async () => {
    const actions = renderQueue();

    fireEvent.click(screen.getAllByRole("button", { name: "Steer" })[1]!);
    await waitFor(() => expect(actions.onSendNow).toHaveBeenCalledWith("task-3"));

    fireEvent.click(screen.getAllByLabelText("More queue actions")[0]!);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Edit queued message" }));
    await waitFor(() => expect(actions.onEdit).toHaveBeenCalledWith("task-2"));

    fireEvent.click(screen.getAllByLabelText("Remove queued message")[1]!);
    await waitFor(() => expect(actions.onRemove).toHaveBeenCalledWith("task-3"));

    fireEvent.click(screen.getAllByLabelText("More queue actions")[1]!);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Clear all" }));
    await waitFor(() => expect(actions.onClear).toHaveBeenCalledTimes(1));
  });

  it("disables send-now until the current positional head is claimable", () => {
    const actions = renderQueue("queued");

    const buttons = screen.getAllByRole("button", {
      name: "Steer is available after the current reply starts",
    });
    expect(buttons).toHaveLength(2);
    for (const button of buttons) expect(button).toBeDisabled();
    expect(actions.onSendNow).not.toHaveBeenCalled();
  });

  it("keeps long queues bounded and blocks duplicate actions while one is pending", async () => {
    let finishClear: (() => void) | undefined;
    const actions = renderQueue();
    actions.onClear.mockReturnValue(new Promise<void>((resolve) => {
      finishClear = resolve;
    }));

    const scroller = actions.container.querySelector('[data-slot="chat-queue-list"]');
    expect(scroller).toHaveClass("max-h-40");

    const clearTrigger = screen.getAllByLabelText("More queue actions")[0]!;
    fireEvent.click(clearTrigger);
    fireEvent.click(await screen.findByRole("menuitem", { name: "Clear all" }));
    await waitFor(() => {
      expect(clearTrigger.querySelector(".animate-spin")).toBeInTheDocument();
      for (const button of screen.getAllByRole("button")) {
        expect(button).toBeDisabled();
      }
    });

    finishClear?.();
    await waitFor(() => {
      expect(clearTrigger.querySelector(".animate-spin")).not.toBeInTheDocument();
      for (const button of screen.getAllByRole("button")) {
        expect(button).toBeEnabled();
      }
    });
    expect(actions.onClear).toHaveBeenCalledTimes(1);
  });
});
