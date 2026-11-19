import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { useCommentRuntimeOriginator } from "./comment-runtime-originator";
import type { MentionItem } from "../../editor/extensions/mention-suggestion";

/** select + args in one step: the composer's actual usage shape. */
function routing_select(
  result: { current: ReturnType<typeof useCommentRuntimeOriginator> },
  item: MentionItem,
) {
  result.current.select(item);
}
function routing_select_then_args(
  result: { current: ReturnType<typeof useCommentRuntimeOriginator> },
  markdown: string,
): ReturnType<ReturnType<typeof useCommentRuntimeOriginator>["requestArgs"]> {
  return result.current.requestArgs(markdown);
}

// A "current task" pick must reach the comment payload as
// runtime_source_task_ids: the sticky continuation routes to that run, not
// to whichever run is newest when the member finally hits Send.
describe("useCommentRuntimeOriginator", () => {
  const currentTaskPick: MentionItem = {
    id: "agent-studio",
    label: "Studio Helper",
    type: "agent",
    group: "issue_runtime",
    agentRuntimeGroup: "issue",
    sourceTaskId: "task-run-1",
  };
  const requesterPick: MentionItem = {
    id: "agent-studio",
    label: "Studio Helper",
    type: "agent",
    group: "requester_runtime",
    agentRuntimeGroup: "requester",
  };

  it("records the picked run for a current-task mention and sends the map", () => {
    const { result } = renderHook(() => useCommentRuntimeOriginator());
    routing_select(result, currentTaskPick);
    const [, sourceTaskIds] = routing_select_then_args(result, 
      "[@Studio Helper](mention://agent/agent-studio) go",
    );
    expect(sourceTaskIds).toEqual({ "agent-studio": "task-run-1" });
    expect(routing_select_then_args(result, "[@Studio Helper](mention://agent/agent-studio) go")[0]).toEqual([]);
  });

  it("switches to a requester override when the member re-picks my runtime", () => {
    const { result } = renderHook(() => useCommentRuntimeOriginator());
    routing_select(result, currentTaskPick);
    routing_select(result, requesterPick);
    const [requesterIds, sourceTaskIds] = routing_select_then_args(result, 
      "[@Studio Helper](mention://agent/agent-studio) go",
    );
    expect(requesterIds).toEqual(["agent-studio"]);
    expect(sourceTaskIds).toBeUndefined();
  });

  it("drops the pick once the mention leaves the draft", () => {
    const { result } = renderHook(() => useCommentRuntimeOriginator());
    routing_select(result, currentTaskPick);
    result.current.prune("no mention here");
    const args = routing_select_then_args(result, "no mention here");
    expect(args.length).toBe(0);
  });

  it("ignores picks for agents that never make it into the payload", () => {
    const { result } = renderHook(() => useCommentRuntimeOriginator());
    routing_select(result, currentTaskPick);
    const args = routing_select_then_args(result, "no mention here");
    expect(args.length).toBe(0);
    expect(routing_select_then_args(result, "[@Studio Helper](mention://agent/agent-studio) go")[1]).toEqual({
      "agent-studio": "task-run-1",
    });
  });
});
