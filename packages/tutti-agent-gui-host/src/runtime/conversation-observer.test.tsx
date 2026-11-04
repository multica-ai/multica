// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { observer } from "mobx-react-lite";
import { afterEach, describe, expect, it } from "vitest";
import { ConversationModel } from "@multica/conversation-client/models";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root | null = null;
afterEach(() => {
  act(() => root?.unmount());
  root = null;
  document.body.replaceChildren();
});

function render(node: React.ReactNode) {
  const container = document.body.appendChild(document.createElement("div"));
  root = createRoot(container);
  act(() => root!.render(node));
}

const at = "2026-11-04T10:00:00Z";
const event = (seq: number, item: string, type: string, payload: Record<string, unknown>) => ({ seq, turn_id: "t", turn_seq: seq, item_id: item, type, final: false, created_at: at, payload });

function model() {
  return ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: 2,
    conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "c", status: "idle", pinned: false, archived: false, updated_at: at },
    turns: [{ id: "t", turn_index: 0, request_id: "r", status: "running", started_at: at, messages: [], events: [
      event(1, "a", "item.started", { kind: "assistant_text" }), event(2, "b", "item.started", { kind: "assistant_text" }),
    ] }],
  }));
}

describe("a conversation under observer components", () => {
  it("renders only the item a batch changed, once per batch", () => {
    const conversation = model();
    const renders: Record<string, number> = { a: 0, b: 0 };
    const Row = observer(function Row({ id }: { id: string }) {
      renders[id]!++;
      return <p data-testid={id}>{conversation.items.get(id)!.text}</p>;
    });
    render(<><Row id="a" /><Row id="b" /></>);
    const before = { ...renders };
    act(() => {
      conversation.applyEvents([event(3, "a", "item.delta", { field: "text", text: "he" }), event(4, "a", "item.delta", { field: "text", text: "llo" })] as never);
    });
    expect(document.querySelector("[data-testid=a]")?.textContent).toBe("hello");
    expect(renders.a).toBe(before.a! + 1);
    expect(renders.b).toBe(before.b);
  });

  it("follows a new snapshot on the same instance", () => {
    const conversation = model();
    const Turns = observer(function Turns() { return <p data-testid="turns">{[...conversation.turns.keys()].join(",")}</p>; });
    render(<Turns />);
    act(() => {
      conversation.replaceSnapshot(localV2SnapshotSchema.parse({
        version: 2, store_generation: "other", snapshot_seq: 9,
        conversation: { conversation_id: "c", summary_seq: 2, provider: "codex", title: "c", status: "idle", pinned: false, archived: false, updated_at: at },
        turns: [{ id: "n", turn_index: 4, request_id: "r", status: "completed", started_at: at, messages: [], events: [] }],
      }));
    });
    expect(document.querySelector("[data-testid=turns]")?.textContent).toBe("n");
  });
});
