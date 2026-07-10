import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { registerCerebroHandlers } from "./handlers";

vi.mock("@multica/core/platform", () => ({ getCurrentWsId: () => "ws-1" }));

describe("round realtime invalidation", () => {
  it("refreshes round progress when issue work or a held response changes", () => {
    const handlers = new Map<string, (payload: unknown) => void>();
    const ws = { on: vi.fn((event: string, handler: (payload: unknown) => void) => { handlers.set(event, handler); return vi.fn(); }) };
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    registerCerebroHandlers(ws as never, qc);
    handlers.get("comment:created")?.({ comment: { issue_id: "issue-1" } });
    handlers.get("task:completed")?.({ issue_id: "issue-1" });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["cerebro-rounds", "ws-1"] });
  });
});
