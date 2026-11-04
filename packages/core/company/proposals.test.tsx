// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { setApiInstance } from "../api";
import { ApiError, type ApiClient } from "../api/client";
import { proposalKeys, useDecideProposal } from "./proposals";

describe("useDecideProposal", () => {
  it("refetches the proposals once a refusal comes back, so a stale view drops its controls", async () => {
    const decideProposal = vi.fn().mockRejectedValue(
      new ApiError("the squad is revising this proposal", 409, "Conflict", { code: "proposal_revising" }),
    );
    setApiInstance({ decideProposal } as unknown as ApiClient);
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: queryClient }, children);
    const { result } = renderHook(() => useDecideProposal("ws-1"), { wrapper });

    await act(async () => {
      await result.current.mutateAsync({ id: "p1", decision: "revise", note: "Cite the numbers" }).catch(() => undefined);
    });

    expect(decideProposal).toHaveBeenCalledWith("p1", { decision: "revise", note: "Cite the numbers" });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: proposalKeys.all("ws-1") });
  });
});
