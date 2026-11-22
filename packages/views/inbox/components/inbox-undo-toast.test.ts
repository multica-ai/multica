import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent } from "@testing-library/react";
import { toast } from "sonner";
import { hasInboxUndo, INBOX_UNDO_TOAST_DURATION, toastInboxUndo } from "./inbox-undo-toast";

vi.mock("sonner", () => {
  let next = 0;
  return { toast: { success: vi.fn(() => ++next), dismiss: vi.fn() } };
});

type Options = {
  duration: number;
  action: { label: string; onClick: () => void };
  onAutoClose: () => void;
};
const lastOptions = () => vi.mocked(toast.success).mock.calls.at(-1)![1] as unknown as Options;
const pressZ = (target: Element = document.body) => fireEvent.keyDown(target, { key: "z", code: "KeyZ" });

describe("toastInboxUndo", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => {
    if (hasInboxUndo()) lastOptions().onAutoClose();
  });

  it("shows the toast long enough to reach its undo", () => {
    toastInboxUndo("Notification archived", { label: "Undo", undo: vi.fn() });
    expect(lastOptions().duration).toBe(INBOX_UNDO_TOAST_DURATION);
    expect(INBOX_UNDO_TOAST_DURATION).toBe(10_000);
  });

  it("undoes the latest action with Z once, and closes its toast", () => {
    const first = vi.fn();
    const latest = vi.fn();
    toastInboxUndo("Notification archived", { label: "Undo", undo: first });
    toastInboxUndo("Snoozed", { label: "Undo", undo: latest });

    pressZ();
    pressZ();

    expect(latest).toHaveBeenCalledOnce();
    expect(first).not.toHaveBeenCalled();
    expect(toast.dismiss).toHaveBeenCalledOnce();
    expect(hasInboxUndo()).toBe(false);
  });

  it("ignores Z once the toast is gone or its button undid it", () => {
    const closed = vi.fn();
    toastInboxUndo("Notification archived", { label: "Undo", undo: closed });
    lastOptions().onAutoClose();
    pressZ();
    expect(closed).not.toHaveBeenCalled();

    const clicked = vi.fn();
    toastInboxUndo("Notification archived", { label: "Undo", undo: clicked });
    lastOptions().action.onClick();
    pressZ();
    expect(clicked).toHaveBeenCalledOnce();
  });

  it("leaves Z to a text field being typed in", () => {
    const undo = vi.fn();
    toastInboxUndo("Notification archived", { label: "Undo", undo });
    const input = document.createElement("input");
    document.body.appendChild(input);
    pressZ(input);
    input.remove();
    expect(undo).not.toHaveBeenCalled();
    expect(hasInboxUndo()).toBe(true);
  });
});
