import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import en from "../../locales/en/runtimes.json";
import { EmptyBuilderRecovery } from "./empty-builder-recovery";

const mocks = vi.hoisted(() => ({ list: vi.fn(), discard: vi.fn() }));
vi.mock("@multica/core/api", async (original) => ({
  ...(await original<typeof import("@multica/core/api")>()),
  api: {
    listEmptyAgentBuilderSessions: mocks.list,
    discardEmptyAgentBuilderSession: mocks.discard,
  },
}));
const row = {
  sessionId: "session1",
  runtimeId: "runtime1",
  machineLabel: "Offline host",
  createdAt: "2026-10-31T12:00:00Z",
  snapshot: "a".repeat(64),
};
beforeEach(() => {
  vi.resetAllMocks();
  mocks.list.mockResolvedValue({ sessions: [row], nextCursor: "" });
  mocks.discard.mockResolvedValue(undefined);
});
afterEach(cleanup);
function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { runtimes: en } }}>
        <EmptyBuilderRecovery wsId="ws1" profileId="profile1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return userEvent.setup();
}
async function discover(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    screen.getByRole("button", { name: en.profiles.recovery.check }),
  );
  await screen.findByText("Offline host");
  await user.click(
    screen.getByRole("button", { name: en.profiles.recovery.discard }),
  );
}
describe("empty Builder recovery", () => {
  it("requires per-session confirmation and preserves cancellation", async () => {
    const user = mount();
    await discover(user);
    expect(mocks.discard).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: en.profiles.recovery.cancel }),
    );
    expect(screen.getByText("Offline host")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: en.profiles.recovery.discard }),
    );
    mocks.list.mockResolvedValue({ sessions: [], nextCursor: "" });
    await user.click(
      screen.getByRole("button", { name: en.profiles.recovery.confirm }),
    );
    await waitFor(() =>
      expect(mocks.discard).toHaveBeenCalledWith(
        "ws1",
        "profile1",
        "session1",
        row.snapshot,
      ),
    );
    await screen.findByText(en.profiles.recovery.empty);
  });
  it("refreshes a 409 snapshot and keeps saved work on the server", async () => {
    const user = mount();
    await discover(user);
    mocks.discard.mockRejectedValue(new ApiError("changed", 409, "Conflict"));
    mocks.list.mockResolvedValue({ sessions: [], nextCursor: "" });
    await user.click(
      screen.getByRole("button", { name: en.profiles.recovery.confirm }),
    );
    await screen.findByText(en.profiles.recovery.changed);
    await screen.findByText(en.profiles.recovery.empty);
    expect(mocks.discard).toHaveBeenCalledTimes(1);
  });
  it.each([403, 500, 404])(
    "keeps the entry and confirmation after a %s",
    async (status) => {
      const user = mount();
      await discover(user);
      mocks.discard.mockRejectedValue(new ApiError("failed", status, "Error"));
      await user.click(
        screen.getByRole("button", { name: en.profiles.recovery.confirm }),
      );
      await waitFor(() =>
        expect(screen.getAllByRole("alert").length).toBeGreaterThan(0),
      );
      expect(screen.getByRole("alertdialog")).toBeInTheDocument();
      expect(mocks.list).toHaveBeenCalledTimes(1);
      expect(mocks.discard).toHaveBeenCalledTimes(1);
    },
  );
  it("reports a missing discovery capability without pretending the list is empty", async () => {
    mocks.list.mockRejectedValue(new ApiError("missing", 404, "Not Found"));
    const user = mount();
    await user.click(
      screen.getByRole("button", { name: en.profiles.recovery.check }),
    );
    await screen.findByText(en.profiles.recovery.unavailable);
    expect(
      screen.queryByText(en.profiles.recovery.empty),
    ).not.toBeInTheDocument();
    expect(mocks.discard).not.toHaveBeenCalled();
  });
});
