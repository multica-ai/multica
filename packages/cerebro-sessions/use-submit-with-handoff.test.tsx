import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: { cerebroRequest: mockCerebroRequest },
  };
});

import { useSubmitWithHandoff } from "./use-sessions";

beforeEach(() => {
  mockCerebroRequest.mockReset();
});

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("useSubmitWithHandoff", () => {
  it("when armed: fires the handoff before posting, then clears", async () => {
    const calls: string[] = [];
    mockCerebroRequest.mockImplementation(async () => {
      calls.push("handoff");
      return {};
    });
    const submit = vi.fn(async () => {
      calls.push("submit");
    });
    const onDone = vi.fn();

    const { result } = renderHook(
      () => useSubmitWithHandoff("issue-1", { rootCommentId: "root-9" }, onDone, submit),
      { wrapper },
    );
    await result.current("hello", ["att-1"]);

    // Handoff (start-fresh) resolves the chosen thread before the new comment posts.
    expect(calls).toEqual(["handoff", "submit"]);
    expect(mockCerebroRequest).toHaveBeenCalledWith(
      expect.stringContaining("/start-fresh"),
      expect.objectContaining({ method: "POST", body: JSON.stringify({ root_comment_id: "root-9" }) }),
    );
    expect(submit).toHaveBeenCalledWith("hello", ["att-1"]);
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it("when not armed: posts directly, no handoff, no clear", async () => {
    const submit = vi.fn(async () => {});
    const onDone = vi.fn();

    const { result } = renderHook(
      () => useSubmitWithHandoff("issue-1", null, onDone, submit),
      { wrapper },
    );
    await result.current("hi");

    expect(mockCerebroRequest).not.toHaveBeenCalled();
    expect(submit).toHaveBeenCalledWith("hi", undefined);
    expect(onDone).not.toHaveBeenCalled();
  });

  it("fails closed: if the handoff errors, the comment is not posted", async () => {
    mockCerebroRequest.mockRejectedValue(new Error("boom"));
    const submit = vi.fn(async () => {});
    const onDone = vi.fn();

    const { result } = renderHook(
      () => useSubmitWithHandoff("issue-1", { rootCommentId: "root-9" }, onDone, submit),
      { wrapper },
    );
    await expect(result.current("hello")).rejects.toThrow("boom");

    expect(submit).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
  });
});
