// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { RuntimeUpdate } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { UpdateSection } from "./update-section";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const mockApi = vi.hoisted(() => ({
  initiateUpdate: vi.fn(),
  getUpdateResult: vi.fn(),
  getActiveUpdate: vi.fn(),
  cancelUpdate: vi.fn(),
}));

vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: mockApi };
});


function renderSection(props: {
  runtimeId: string | null;
  launchedBy?: string | null;
  currentVersion?: string;
}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <UpdateSection
          runtimeId={props.runtimeId}
          currentVersion={props.currentVersion ?? "v0.4.0"}
          isOnline
          launchedBy={props.launchedBy}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockApi.initiateUpdate.mockReset();
  mockApi.getUpdateResult.mockReset();
  mockApi.cancelUpdate.mockReset();
  mockApi.getActiveUpdate.mockReset().mockResolvedValue(null);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("UpdateSection read-only status", () => {
  it("shows Latest without a redundant read-only label or update action", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: "v0.4.0" }),
      }),
    );

    renderSection({ runtimeId: null });

    expect(await screen.findByText("Latest")).toBeInTheDocument();
    expect(screen.queryByText("Read-only")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update" }),
    ).not.toBeInTheDocument();
  });

  it("shows the Desktop manager without exposing an update action", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: "v0.4.0" }),
      }),
    );

    renderSection({ runtimeId: null, launchedBy: "desktop" });

    expect(screen.getByText("Managed by Desktop")).toBeInTheDocument();
    expect(screen.queryByText("Read-only")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update" }),
    ).not.toBeInTheDocument();
  });

  it("shows an available version without an action for a read-only viewer", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: "v0.4.0" }),
      }),
    );

    renderSection({ runtimeId: null, currentVersion: "v0.3.17" });

    expect(await screen.findByText("available")).toBeInTheDocument();
    expect(screen.getByText("Read-only")).toBeInTheDocument();
    expect(
      screen.getByTitle(
        "Only runtime owners and workspace admins can update the CLI.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update" }),
    ).not.toBeInTheDocument();
  });
});

// A daemon built from source reports either the ldflags default ("dev") or a
// `git describe` string. Neither can be ordered against a release tag, so the
// UI must not claim an upgrade is available — a manual update is legitimate
// precisely because a human made an informed decision, and that decision has to
// rest on something we actually parsed.
describe("UpdateSection non-release versions", () => {
  const LATEST = "v0.4.20";

  // fetchLatestVersion memoizes the GitHub tag in module scope for 10 minutes,
  // so without advancing the clock every case here would silently reuse the tag
  // an earlier test cached rather than its own.
  let clock = Date.now();

  beforeEach(() => {
    clock += 60 * 60 * 1000;
    vi.spyOn(Date, "now").mockReturnValue(clock);
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: LATEST }),
      }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const NON_RELEASE_VERSIONS = [
    "dev",
    "v0.4.17-12-gabc1234",
    "v0.4.17-dirty",
    "v0.4.17-rc1",
    "v0.4",
  ];

  for (const currentVersion of NON_RELEASE_VERSIONS) {
    it(`offers no update and claims no state for "${currentVersion}"`, async () => {
      renderSection({ runtimeId: "runtime-1", currentVersion });

      expect(await screen.findByText("Local build")).toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Update" }),
      ).not.toBeInTheDocument();
      expect(screen.queryByText("available")).not.toBeInTheDocument();
      // "Latest" would be just as unfounded as "update available".
      expect(screen.queryByText("Latest")).not.toBeInTheDocument();
    });
  }

  it("still offers the update on a release version behind the latest", async () => {
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.17" });

    expect(
      await screen.findByRole("button", { name: "Update" }),
    ).toBeInTheDocument();
    expect(screen.getByText("available")).toBeInTheDocument();
    expect(screen.queryByText("Local build")).not.toBeInTheDocument();
  });

  it("reports a release version on the latest tag as Latest", async () => {
    renderSection({ runtimeId: "runtime-1", currentVersion: LATEST });

    expect(await screen.findByText("Latest")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Local build")).not.toBeInTheDocument();
  });
});

describe("UpdateSection queued updates", () => {
  const LATEST = "v0.9.2";
  let clock = Date.now() + 365 * 24 * 60 * 60 * 1000;

  beforeEach(() => {
    // Expire fetchLatestVersion's module cache so this suite sees its own tag.
    clock += 60 * 60 * 1000;
    vi.spyOn(Date, "now").mockReturnValue(clock);
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: LATEST }),
      }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function request(overrides: Partial<RuntimeUpdate> = {}): RuntimeUpdate {
    return {
      id: "upd-1",
      runtime_id: "runtime-1",
      status: "scheduled",
      target_version: LATEST,
      scheduled_at: new Date(clock - 18 * 60_000).toISOString(),
      active_tasks: 2,
      created_at: new Date(clock - 18 * 60_000).toISOString(),
      updated_at: new Date(clock).toISOString(),
      ...overrides,
    };
  }

  function follow(update: RuntimeUpdate) {
    mockApi.getActiveUpdate.mockResolvedValue(update);
    mockApi.getUpdateResult.mockResolvedValue(update);
  }

  it("shows a queued update found on mount as waiting, not failed", async () => {
    follow(request());
    const { container } = renderSection({
      runtimeId: "runtime-1",
      currentVersion: "v0.9.0",
    });

    expect(await screen.findByText("Updates when idle")).toBeInTheDocument();
    expect(mockApi.getActiveUpdate).toHaveBeenCalledWith("runtime-1");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.getByText("Wait for tasks to finish")).toBeInTheDocument();
    expect(screen.getByText(/^2 running · /)).toBeInTheDocument();
    expect(
      screen.getByText("Running tasks won't be interrupted."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Update failed")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();
    expect(container.querySelector(".text-destructive")).toBeNull();
  });

  it("omits the task count when the daemon cannot report one", async () => {
    follow(request({ active_tasks: undefined }));
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    expect(await screen.findByText(/^Waiting /)).toBeInTheDocument();
    expect(screen.queryByText(/running ·/)).not.toBeInTheDocument();
  });

  it("cancels a queued update and offers the update again", async () => {
    follow(request());
    mockApi.cancelUpdate.mockResolvedValue(request({ status: "cancelled" }));
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(mockApi.cancelUpdate).toHaveBeenCalledWith("runtime-1", "upd-1"),
    );
    expect(await screen.findByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(screen.queryByText("Updates when idle")).not.toBeInTheDocument();
  });

  it("queues instead of failing when the machine is busy", async () => {
    mockApi.initiateUpdate.mockResolvedValue(
      request({ status: "pending", scheduled_at: undefined, active_tasks: undefined }),
    );
    mockApi.getUpdateResult.mockResolvedValue(request());
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    fireEvent.click(await screen.findByRole("button", { name: "Update" }));

    await waitFor(() =>
      expect(mockApi.initiateUpdate).toHaveBeenCalledWith("runtime-1", LATEST),
    );
    expect(await screen.findByText("Updates when idle")).toBeInTheDocument();
  });

  it("drops Cancel once installation starts", async () => {
    follow(request({ status: "running", run_started_at: new Date(clock).toISOString() }));
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    expect(await screen.findByText("Updating...")).toBeInTheDocument();
    expect(screen.getByText("Tasks finished")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
  });

  it("keeps a real failure red with Retry", async () => {
    follow(
      request({
        status: "failed",
        error: "brew upgrade failed: exit status 1",
      }),
    );
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    const message = await screen.findByText("brew upgrade failed: exit status 1");
    expect(message).toHaveClass("text-destructive");
    expect(screen.getByText("Update failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  // A server from before queued updates still reports the busy deferral as a
  // failure. Its internal wording must not reach the page, and it is not red.
  it("never shows the raw busy error from an older server", async () => {
    follow(
      request({
        status: "failed",
        error:
          "runtime update deferred because agent work is starting or still active; retry when the machine is idle",
      }),
    );
    const { container } = renderSection({
      runtimeId: "runtime-1",
      currentVersion: "v0.9.0",
    });

    expect(
      await screen.findByText(
        "The machine was busy, so the update didn't start. Try again when it's idle.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/runtime update deferred/)).not.toBeInTheDocument();
    expect(screen.queryByText("Update failed")).not.toBeInTheDocument();
    expect(container.querySelector(".text-destructive")).toBeNull();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("explains an expired queue without red and offers the update again", async () => {
    mockApi.initiateUpdate.mockResolvedValue(request({ status: "pending" }));
    mockApi.getUpdateResult.mockResolvedValue(request({ status: "expired" }));
    const { container } = renderSection({
      runtimeId: "runtime-1",
      currentVersion: "v0.9.0",
    });

    fireEvent.click(await screen.findByRole("button", { name: "Update" }));

    expect(await screen.findByText("Queue expired")).toBeInTheDocument();
    expect(
      screen.getByText("Expired after waiting 24 hours for the machine to go idle."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(container.querySelector(".text-destructive")).toBeNull();
  });

  it("works against a server without the active-update endpoint", async () => {
    // The API client maps that server's 404/405 to null (covered in core).
    mockApi.getActiveUpdate.mockResolvedValue(null);
    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.9.0" });

    expect(await screen.findByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(mockApi.getUpdateResult).not.toHaveBeenCalled();
  });
});
