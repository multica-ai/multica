import { useState } from "react";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { SnoozedInboxIssue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { useInboxSnooze } from "./use-inbox-snooze";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/api", () => ({ api: { snoozeInbox: vi.fn(), unsnoozeInbox: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const earlier = new Date("2026-11-20T09:00:00Z");
const later = new Date("2026-11-21T09:00:00Z");

function queryClient(snoozed: SnoozedInboxIssue[] = []) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  });
  qc.setQueryData(inboxKeys.snoozed("ws"), snoozed);
  return qc;
}

const snoozedEntry: SnoozedInboxIssue = {
  issue_id: "i1",
  item_id: "n1",
  snoozed_until: earlier.toISOString(),
  snoozed_at: "2026-11-16T00:00:00Z",
  updates: 2,
};

function Snoozer({ read, onSnoozed }: { read?: boolean; onSnoozed?: () => void }) {
  const { snooze } = useInboxSnooze();
  return (
    <button
      type="button"
      onClick={() => {
        snooze({ id: "n1", read }, later);
        onSnoozed?.();
      }}
    >
      Snooze
    </button>
  );
}

/** The snoozing component goes away right after, as the issue header does when it opens the next issue. */
function Leaving() {
  const [here, setHere] = useState(true);
  return here ? <Snoozer onSnoozed={() => setHere(false)} /> : <span>Next issue</span>;
}

function undoFromToast() {
  const action = vi.mocked(toast.success).mock.calls[0]![1]!.action as unknown as { onClick: () => void };
  return act(async () => action.onClick());
}

describe("useInboxSnooze", () => {
  beforeEach(() => {
    // Only the clock is faked, so promises and waitFor still run.
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-11-16T06:20:00Z"));
    vi.clearAllMocks();
    vi.mocked(api.snoozeInbox).mockResolvedValue({ issue_id: "i1", snoozed_until: later.toISOString() });
    vi.mocked(api.unsnoozeInbox).mockResolvedValue({ issue_id: "i1" });
  });
  afterEach(() => vi.useRealTimers());

  it("still shows the toast with Undo after the snoozing component unmounts", async () => {
    let finish!: (value: { issue_id: string; snoozed_until: string }) => void;
    vi.mocked(api.snoozeInbox).mockImplementation(() => new Promise((resolve) => (finish = resolve)));
    renderWithI18n(
      <QueryClientProvider client={queryClient()}>
        <Leaving />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByText("Snooze"));
    expect(screen.getByText("Next issue")).toBeInTheDocument();
    await waitFor(() => expect(api.snoozeInbox).toHaveBeenCalled());
    await act(async () => finish({ issue_id: "i1", snoozed_until: later.toISOString() }));

    expect(toast.success).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ action: expect.objectContaining({ label: "Undo" }) }),
    );
  });

  it("still reports a failure after the snoozing component unmounts", async () => {
    vi.mocked(api.snoozeInbox).mockRejectedValue(new Error("Snooze rejected"));
    renderWithI18n(
      <QueryClientProvider client={queryClient()}>
        <Leaving />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByText("Snooze"));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Snooze rejected"));
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("undoes a first snooze by bringing the issue back as it was", async () => {
    renderWithI18n(
      <QueryClientProvider client={queryClient()}>
        <Snoozer read />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByText("Snooze"));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    await undoFromToast();

    expect(api.unsnoozeInbox).toHaveBeenCalledWith("n1", { read: true });
    expect(api.snoozeInbox).toHaveBeenCalledTimes(1);
  });

  it("undoes a change of time by putting back the previous time", async () => {
    renderWithI18n(
      <QueryClientProvider client={queryClient([snoozedEntry])}>
        <Snoozer />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByText("Snooze"));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    await undoFromToast();

    expect(api.snoozeInbox).toHaveBeenLastCalledWith("n1", earlier.toISOString());
    expect(api.unsnoozeInbox).not.toHaveBeenCalled();
  });
});
