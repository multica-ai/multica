/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { ApiError } from "@multica/core/api/client";
import type { ApiClient } from "@multica/core/api/client";

const toastError = vi.fn();
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({
  toast: {
    error: (msg: string) => toastError(msg),
    success: (msg: string) => toastSuccess(msg),
  },
}));

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
  return {
    ...renderHook(() => useIssueSubscribers("issue-1", "user-1"), {
      wrapper: wrapper(queryClient),
    }),
    queryClient,
  };
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
    toastSuccess.mockClear();
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

  // Unlike the direct toggle, this mutation is not optimistic — no label or
  // avatar flips to confirm the click landed, and the descendants it retired
  // are not on screen at all. Silence here is indistinguishable from a dead
  // button, which is the MUL-5710 complaint (MUL-5714).
  it("confirms a successful subtree unsubscribe", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      unsubscribeFromIssueSubtree: async () => undefined,
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    result.current.unsubscribeFromSubtree();

    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        "detail.unsubscribe_subtree_succeeded",
      ),
    );
    expect(toastError).not.toHaveBeenCalled();
  });
});

const SUBSCRIBED_AS_MEMBER = [
  {
    issue_id: "issue-1",
    user_type: "member",
    user_id: "user-1",
    reason: "manual",
    created_at: "2026-01-01T00:00:00Z",
  },
];

/**
 * useToggleIssueSubscriber patches the cache optimistically and rolls that
 * patch back when the request fails, which restores the exact row the user
 * started from. Without a message that is pixel-identical to a button that
 * never fired — the same symptom MUL-5710 was reported as (MUL-5714).
 */
describe("useIssueSubscribers direct toggle feedback", () => {
  afterEach(() => {
    cleanup();
    toastError.mockClear();
    toastSuccess.mockClear();
  });

  it("reports a failed unsubscribe", async () => {
    setApiInstance({
      listIssueSubscribers: async () => SUBSCRIBED_AS_MEMBER,
      unsubscribeFromIssue: async () => {
        throw new ApiError("boom", 500, "Internal Server Error");
      },
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    await waitFor(() => expect(result.current.isSubscribed).toBe(true));
    result.current.toggleSubscribe();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "detail.subscription_update_failed",
      ),
    );
  });

  it("reports a failed subscribe", async () => {
    setApiInstance({
      listIssueSubscribers: async () => [],
      subscribeToIssue: async () => {
        throw new ApiError("boom", 500, "Internal Server Error");
      },
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    await waitFor(() => expect(result.current.subscriptionKnown).toBe(true));
    result.current.toggleSubscribe();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        "detail.subscription_update_failed",
      ),
    );
  });

  // The optimistic patch IS the success feedback here; a toast on top of a
  // button that already flipped is noise on a control people press often.
  it("stays quiet when the toggle succeeds", async () => {
    setApiInstance({
      listIssueSubscribers: async () => SUBSCRIBED_AS_MEMBER,
      unsubscribeFromIssue: async () => undefined,
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    await waitFor(() => expect(result.current.isSubscribed).toBe(true));
    result.current.toggleSubscribe();

    await waitFor(() => expect(result.current.togglePending).toBe(false));
    expect(toastError).not.toHaveBeenCalled();
    expect(toastSuccess).not.toHaveBeenCalled();
  });
});

/**
 * `isSubscribed` is derived from `data ?? []`, so it reads false for everyone
 * until the query resolves. `subscriptionKnown` is what lets the UI tell that
 * default apart from a real answer (MUL-5714).
 */
describe("useIssueSubscribers subscriptionKnown", () => {
  afterEach(() => {
    cleanup();
    toastError.mockClear();
    toastSuccess.mockClear();
  });

  it("is false until the subscribers query resolves", async () => {
    setApiInstance({
      listIssueSubscribers: async () => SUBSCRIBED_AS_MEMBER,
    } as unknown as ApiClient);

    const { result } = renderSubscribers();
    // The very first render already carries the misleading default.
    expect(result.current.isSubscribed).toBe(false);
    expect(result.current.subscriptionKnown).toBe(false);

    await waitFor(() => expect(result.current.subscriptionKnown).toBe(true));
    expect(result.current.isSubscribed).toBe(true);
  });

  it("stays false when the subscribers query fails", async () => {
    setApiInstance({
      listIssueSubscribers: async () => {
        throw new ApiError("boom", 500, "Internal Server Error");
      },
    } as unknown as ApiClient);

    const { result, queryClient } = renderSubscribers();

    await waitFor(() =>
      expect(
        queryClient.getQueryState(issueKeys.subscribers("issue-1"))?.status,
      ).toBe("error"),
    );
    expect(result.current.subscriptionKnown).toBe(false);
  });
});
