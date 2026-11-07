// @vitest-environment node
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { groupTranscriptActivities, transcript } from "./projection";
import { useCompletionRead } from "./use-completion-read";

const native = vi.hoisted(() => ({
  currentState: "active",
  listener: undefined as ((state: string) => void) | undefined,
  setup: undefined as (() => void | (() => void)) | undefined,
  visibleRows: [] as string[],
  acknowledged: { current: new Set<string>() },
  remove: vi.fn(),
}));

// Run the actual hook effect, native subscription and FlashList callback without rendering RN.
vi.mock("react", () => ({
  useState: () => [native.visibleRows, (update: (previous: string[]) => string[]) => { native.visibleRows = update(native.visibleRows); }],
  useRef: () => native.acknowledged,
  useCallback: <T>(callback: T) => callback,
  useEffect: (setup: () => void | (() => void)) => { native.setup = setup; },
}));
vi.mock("react-native", () => ({
  AppState: {
    get currentState() { return native.currentState; },
    addEventListener: (_event: string, listener: (state: string) => void) => {
      native.listener = listener;
      return { remove: () => {
        if (native.listener === listener) native.listener = undefined;
        native.remove();
      } };
    },
  },
}));

function completedSnapshot() {
  return localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: 3,
    conversation: { conversation_id: "c", summary_seq: 3, provider: "claude", title: "c", status: "idle", pinned: false, archived: false, updated_at: "now",
      completion: { turn_id: "a", turn_index: 0, completion_seq: 3 }, read_completion_seq: 0 },
    turns: [{ id: "a", turn_index: 0, status: "completed", started_at: "now", messages: [], events: [
      { seq: 1, type: "item.completed", final: true, turn_id: "a", item_id: "explanation", payload: { kind: "assistant_text", text: "I will inspect the files." }, created_at: "now" },
      { seq: 2, type: "item.completed", final: true, turn_id: "a", item_id: "answer", payload: { kind: "assistant_text", text: "Here is the result." }, created_at: "now" },
    ] }],
  });
}

let cleanup: (() => void) | undefined;
beforeEach(() => {
  native.currentState = "active";
  native.listener = undefined;
  native.setup = undefined;
  native.visibleRows = [];
  native.acknowledged = { current: new Set() };
  native.remove.mockClear();
});
afterEach(() => { cleanup?.(); cleanup = undefined; });

function mount() {
  const options = {
    client: { completionReadSupported: true, storeGeneration: "g", readCompletion: vi.fn(async () => {}) },
    session: "c", snapshot: completedSnapshot(), enabled: true,
    rows: groupTranscriptActivities(transcript(completedSnapshot())),
  };
  function render() {
    cleanup?.();
    const viewable = useCompletionRead(options);
    cleanup = native.setup?.() || undefined;
    return viewable;
  }
  render();
  return {
    options, render,
    show: (...ids: string[]) => {
      render()({ viewableItems: options.rows.filter((row) => ids.includes(row.id))
        .map((item, index) => ({ item, key: item.id, index, isViewable: true })) });
      render();
    },
    appState: (state: string) => { native.currentState = state; native.listener?.(state); },
  };
}

it.each(["background", "inactive"])("does not read a completion arriving while %s and rechecks on foreground recovery", (state) => {
  const screen = mount();
  screen.options.snapshot.conversation.completion = null;
  screen.options.snapshot.turns[0].status = "running";
  screen.show("answer");
  screen.appState(state);
  screen.options.snapshot = completedSnapshot();
  screen.render();
  expect(screen.options.client.readCompletion).not.toHaveBeenCalled();
  screen.appState("active");
  expect(screen.options.client.readCompletion).toHaveBeenCalledExactlyOnceWith("c", "g", "a", 0, 3);
  screen.appState(state);
  screen.appState("active");
  expect(screen.options.client.readCompletion).toHaveBeenCalledOnce();
});

it("keeps row identity through the FlashList callback until the final answer is visible", () => {
  const screen = mount();
  screen.show("explanation");
  expect(screen.options.client.readCompletion).not.toHaveBeenCalled();
  screen.show("answer");
  expect(screen.options.client.readCompletion).toHaveBeenCalledExactlyOnceWith("c", "g", "a", 0, 3);
});

it("does not read when mounted inactive, when the screen is disabled, or after unmount", () => {
  native.currentState = "inactive";
  const screen = mount();
  screen.show("answer");
  expect(screen.options.client.readCompletion).not.toHaveBeenCalled();
  screen.options.enabled = false;
  screen.render();
  screen.appState("active");
  expect(screen.options.client.readCompletion).not.toHaveBeenCalled();
  screen.options.enabled = true;
  screen.render();
  expect(screen.options.client.readCompletion).toHaveBeenCalledOnce();
  cleanup?.(); cleanup = undefined;
  expect(native.listener).toBeUndefined();
  screen.appState("active");
  expect(screen.options.client.readCompletion).toHaveBeenCalledOnce();
});

it("retries a failed confirmation when returning to the foreground", async () => {
  const screen = mount();
  screen.options.client.readCompletion.mockRejectedValueOnce(new Error("offline"));
  screen.show("answer");
  await Promise.resolve();
  screen.appState("background");
  screen.appState("active");
  expect(screen.options.client.readCompletion).toHaveBeenCalledTimes(2);
});
