/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import { ApiError } from "@multica/core/api/client";
import type { ApiClient } from "@multica/core/api/client";

const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (msg: string) => toastError(msg) } }));

// Return the key path so an assertion can tell the two failure messages apart
// without depending on the English copy.
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (sel: (d: Record<string, Record<string, string>>) => string) =>
      sel(
        new Proxy(
          {},
          {
            get: (_t, section: string) =>
              new Proxy(
                {},
                { get: (_s, key: string) => `${section}.${key}` },
              ),
          },
        ) as Record<string, Record<string, string>>,
      ),
  }),
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: () => undefined,
  useWSReconnect: () => undefined,
}));

import { useIssueSubscribers } from "./use-issue-subscribers";

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

function renderSubscribers() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderHook(() => useIssueSubscribers("issue-1", "user-1"), {
    wrapper: wrapper(queryClient),
  });
}

/**
 * Subtree unsubscribe is not optimistic — nothing on screen changes when it
 * fails — so without an explicit message a failure is indistinguishable from a
 * dead button. The 404 case specifically means the backend predates the
 * feature (web/desktop staging deploys on merge, backend by hand), which the
 * user can only wait out, so it must not read as a generic error
 * (MUL-5483 review round 7).
 */
describe("useIssueSubscribers subtree unsubscribe failures", () => {
  afterEach(() => {
    cleanup();
    toastError.mockClear();
  });

  it("tells the user when the backend has no subtree route yet", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      unsubscribeFromIssueSubtree: async () => {
        // chi answers an unknown route with plain text, so parseErrorBody
        // leaves body undefined. That is the deploy-skew signature.
        throw new ApiError("not found", 404, "Not Found");
      },
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    result.current.unsubscribeFromSubtree();

    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
    expect(toastError).toHaveBeenCalledWith(
      "detail.unsubscribe_subtree_unsupported",
    );
  });

  it("does not blame deploy skew for a structured 404 from a current backend", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      unsubscribeFromIssueSubtree: async () => {
        // The route exists; the ISSUE is gone or not visible. Telling this user
        // to wait for the next deploy would be wrong advice.
        throw new ApiError("issue not found", 404, "Not Found", {
          error: "issue not found",
        });
      },
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    result.current.unsubscribeFromSubtree();

    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
    expect(toastError).toHaveBeenCalledWith("detail.unsubscribe_subtree_failed");
  });

  it("falls back to a generic failure for any other error", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      unsubscribeFromIssueSubtree: async () => {
        throw new ApiError("boom", 500, "Internal Server Error");
      },
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    result.current.unsubscribeFromSubtree();

    await waitFor(() => expect(toastError).toHaveBeenCalledTimes(1));
    expect(toastError).toHaveBeenCalledWith("detail.unsubscribe_subtree_failed");
  });

  it("stays silent when the unsubscribe succeeds", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      unsubscribeFromIssueSubtree: async () => undefined,
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    result.current.unsubscribeFromSubtree();

    await waitFor(() => expect(toastError).not.toHaveBeenCalled());
  });
});
