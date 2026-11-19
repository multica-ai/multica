// @vitest-environment node
import { MutationObserver, QueryClient } from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
import { useSessionMutation } from "./use-session-mutation";
import { invalidateSessionEpoch } from "./session-epoch";

const state = vi.hoisted(() => ({ qc: undefined as unknown as QueryClient }));
vi.mock("@tanstack/react-query", async (original) => {
  const actual = await original<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useMutation: (options: ConstructorParameters<typeof MutationObserver>[1]) => {
      const observer = new actual.MutationObserver(state.qc, options);
      observer.subscribe(() => {});
      return { mutateAsync: observer.mutate.bind(observer) };
    },
  };
});

it("keeps concurrent invocation epochs separate and suppresses old lifecycle and caller callbacks", async () => {
  state.qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const requests = new Map<string, { resolve: (data: string) => void; reject: (error: Error) => void }>();
  const success = vi.fn();
  const error = vi.fn();
  const settled = vi.fn();
  const oldCaller = vi.fn();
  const newCaller = vi.fn();
  const mutation = useSessionMutation({
    mutationFn: (name: string) => new Promise<string>((resolve, reject) => requests.set(name, { resolve, reject })),
    onSuccess: success, onError: error, onSettled: settled,
  });
  const a = mutation.mutateAsync("a", { onSuccess: oldCaller, onError: oldCaller, onSettled: oldCaller }).catch((e) => e);
  await vi.waitFor(() => expect(requests.has("a")).toBe(true));
  invalidateSessionEpoch();
  state.qc.clear();
  const b = mutation.mutateAsync("b", { onSuccess: newCaller });
  await vi.waitFor(() => expect(requests.has("b")).toBe(true));
  requests.get("a")!.resolve("private A");
  expect(await a).toMatchObject({ message: "Server session changed" });
  expect(success).not.toHaveBeenCalled();
  expect(error).not.toHaveBeenCalled();
  expect(settled).not.toHaveBeenCalled();
  expect(oldCaller).not.toHaveBeenCalled();
  requests.get("b")!.resolve("B");
  expect(await b).toBe("B");
  expect(success).toHaveBeenCalledOnce();
  expect(settled).toHaveBeenCalledOnce();
  expect(newCaller).toHaveBeenCalledOnce();
  state.qc.clear();
});
