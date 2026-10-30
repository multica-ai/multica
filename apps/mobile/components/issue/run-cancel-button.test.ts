// @vitest-environment node
import { Alert } from "react-native";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCancelTask } from "@/data/mutations/issues";
import { RunCancelButton } from "./run-cancel-button";

const state = vi.hoisted(() => ({ submitted: { current: false } }));
vi.mock("react", async (importOriginal) => ({
  ...await importOriginal<typeof import("react")>(),
  useRef: () => state.submitted,
}));
vi.mock("react-native", () => ({
  Alert: { alert: vi.fn() }, Pressable: "Pressable", View: "View",
}));
vi.mock("@/components/ui/text", () => ({ Text: "Text" }));
vi.mock("@/lib/i18n", () => ({ useT: () => ({ t: (key: string) => key }) }));
vi.mock("@/data/mutations/issues", () => ({ useCancelTask: vi.fn() }));

const mutation = {
  isPending: false, isPaused: false, isSuccess: false, isError: false,
  mutate: vi.fn(),
};

// Inspect props and invoke callbacks without pretending to render native UI.
function render() {
  const tree = RunCancelButton({ taskId: "task-1", issueId: "issue-1" });
  return { button: tree.props.children[0], error: tree.props.children[1] };
}

function confirm() {
  const buttons = vi.mocked(Alert.alert).mock.lastCall?.[2];
  buttons?.find((button) => button.style === "destructive")?.onPress?.();
}

describe("RunCancelButton wiring", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.submitted.current = false;
    Object.assign(mutation, { isPending: false, isPaused: false, isSuccess: false, isError: false });
    vi.mocked(useCancelTask).mockReturnValue(mutation as unknown as ReturnType<typeof useCancelTask>);
  });

  it("confirms the specific run and prevents repeated native callbacks before a rerender", () => {
    const { button } = render();
    expect(useCancelTask).toHaveBeenCalledWith("issue-1");
    expect(button.props.disabled).toBe(false);
    button.props.onPress();
    expect(mutation.mutate).not.toHaveBeenCalled();
    confirm();
    confirm();
    button.props.onPress();
    expect(mutation.mutate).toHaveBeenCalledExactlyOnceWith("task-1", expect.any(Object));
    expect(Alert.alert).toHaveBeenCalledOnce();
  });

  it.each([
    [{ isPending: true }, "cancel.stopping", true],
    [{ isPending: true, isPaused: true }, "cancel.waiting_for_connection", true],
    [{ isSuccess: true }, "cancel.stopping", false],
  ])("shows a disabled waiting state for %j", (flags, label, busy) => {
    Object.assign(mutation, flags);
    const { button, error } = render();
    expect(button.props.disabled).toBe(true);
    expect(button.props.accessibilityLabel).toBe(label);
    expect(button.props.children.props.children).toBe(label);
    expect(button.props.accessibilityState).toEqual({ disabled: true, busy });
    button.props.onPress();
    expect(Alert.alert).not.toHaveBeenCalled();
    expect(error).toBe(null);
  });

  it("shows an actionable failure and allows confirmation again after rejection", () => {
    render().button.props.onPress();
    confirm();
    mutation.mutate.mock.calls[0][1].onError(new Error("denied"));
    mutation.isError = true;
    const { button, error } = render();
    expect(error.props.accessibilityRole).toBe("alert");
    expect(error.props.children).toBe("cancel.failed");
    expect(button.props.disabled).toBe(false);
    button.props.onPress();
    confirm();
    expect(mutation.mutate).toHaveBeenCalledTimes(2);
  });

  it("keeps the native keep-running action free of writes", () => {
    render().button.props.onPress();
    const buttons = vi.mocked(Alert.alert).mock.lastCall?.[2];
    expect(buttons?.find((button) => button.style === "cancel")).toMatchObject({
      text: "cancel.keep_running",
    });
    expect(mutation.mutate).not.toHaveBeenCalled();
  });
});
