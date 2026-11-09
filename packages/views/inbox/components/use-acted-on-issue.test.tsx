import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useActedOnIssue } from "./use-acted-on-issue";

type Handler = (payload: unknown, actorId?: string, actorType?: string) => void;
const wsHandlers = vi.hoisted(() => new Map<string, Handler>());
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: Handler) => {
    wsHandlers.set(event, handler);
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "me" } }),
}));

function comment(issueId: string, authorId: string, authorType = "member") {
  return { comment: { issue_id: issueId, author_id: authorId, author_type: authorType } };
}

function emit(event: string, payload: unknown, actorId?: string, actorType?: string) {
  act(() => wsHandlers.get(event)?.(payload, actorId, actorType));
}

describe("useActedOnIssue", () => {
  it("notices the viewer's own reply on the open issue", () => {
    const { result } = renderHook(() => useActedOnIssue("issue-a"));
    expect(result.current).toBeNull();

    emit("comment:created", comment("issue-a", "me"));
    expect(result.current).toBe("replied");
  });

  it("ignores replies by others or on other issues", () => {
    const { result } = renderHook(() => useActedOnIssue("issue-a"));

    emit("comment:created", comment("issue-a", "someone-else"));
    emit("comment:created", comment("issue-a", "me", "agent"));
    emit("comment:created", comment("issue-b", "me"));
    expect(result.current).toBeNull();
  });

  it("notices the viewer changing status or assignee, not other edits", () => {
    const { result } = renderHook(() => useActedOnIssue("issue-a"));

    emit("issue:updated", { issue: { id: "issue-a" }, title_changed: true }, "me", "member");
    expect(result.current).toBeNull();
    emit("issue:updated", { issue: { id: "issue-a" }, status_changed: true }, "someone-else", "member");
    expect(result.current).toBeNull();

    emit("issue:updated", { issue: { id: "issue-a" }, assignee_changed: true }, "me", "member");
    expect(result.current).toBe("updated");
  });

  it("forgets the action once another issue opens", () => {
    const { result, rerender } = renderHook(({ id }) => useActedOnIssue(id), {
      initialProps: { id: "issue-a" as string | null },
    });
    emit("comment:created", comment("issue-a", "me"));
    expect(result.current).toBe("replied");

    rerender({ id: "issue-b" });
    expect(result.current).toBeNull();
  });
});
