// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { api, ApiError } from "@multica/core/api";
import type { RuntimeUpdate } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { UpdateSection } from "./update-section";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

vi.mock("@multica/core/api", async (importOriginal) => ({
  ApiError: (await importOriginal<typeof import("@multica/core/api")>()).ApiError,
  api: {
    initiateUpdate: vi.fn(),
    getUpdateResult: vi.fn(),
    getActiveUpdate: vi.fn().mockResolvedValue(null),
    cancelUpdate: vi.fn(),
  },
}));

function renderSection(props: {
  runtimeId: string | null;
  launchedBy?: string | null;
  currentVersion?: string;
}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <UpdateSection
        runtimeId={props.runtimeId}
        currentVersion={props.currentVersion ?? "v0.4.0"}
        isOnline
        launchedBy={props.launchedBy}
      />
    </I18nProvider>,
  );
}

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
  let clock = Date.now() + 100 * 60 * 60 * 1000;

  function update(overrides: Partial<RuntimeUpdate>): RuntimeUpdate {
    return {
      id: "update-1",
      runtime_id: "runtime-1",
      status: "scheduled",
      target_version: "v0.5.0",
      cancellable: true,
      created_at: "",
      updated_at: "",
      ...overrides,
    };
  }

  beforeEach(() => {
    clock += 60 * 60 * 1000;
    vi.spyOn(Date, "now").mockReturnValue(clock);
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ tag_name: "v0.5.0" }) }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.mocked(api.getActiveUpdate).mockResolvedValue(null);
  });

  it("shows a queued update from the server and cancels it", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(update({ active_tasks: 2 }));
    vi.mocked(api.cancelUpdate).mockResolvedValue(update({ status: "cancelled", cancellable: false }));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });

    expect(await screen.findByText("Updates when idle")).toBeInTheDocument();
    expect(screen.getByText(/2 running · Running tasks won't be interrupted\./)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Update" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(await screen.findByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(api.cancelUpdate).toHaveBeenCalledWith("runtime-1", "update-1");
    expect(screen.queryByText("Updates when idle")).not.toBeInTheDocument();
  });

  it("says so when installation already started before the cancel", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(update({}));
    vi.mocked(api.cancelUpdate).mockRejectedValue(new ApiError("already started", 409, "Conflict"));
    vi.mocked(api.getUpdateResult).mockResolvedValue(update({ status: "running", cancellable: false }));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(await screen.findByText("Couldn't cancel: installation has already started.")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Updating...")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
  });

  it("queues an update clicked while busy", async () => {
    vi.mocked(api.initiateUpdate).mockResolvedValue(update({ status: "pending" }));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });
    fireEvent.click(await screen.findByRole("button", { name: "Update" }));

    expect(await screen.findByText("Waiting for daemon...")).toBeInTheDocument();
    expect(api.initiateUpdate).toHaveBeenCalledWith("runtime-1", "v0.5.0");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
  });

  it("shows a busy refusal from a daemon that cannot queue as a neutral note", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(
      update({
        status: "failed",
        cancellable: false,
        error: "runtime update deferred because agent work is starting or still active; retry when the machine is idle",
      }),
    );

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });

    expect(await screen.findByText(/The machine was busy/)).toBeInTheDocument();
    expect(screen.queryByText("Update failed")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update" })).toBeInTheDocument();
  });

  it("drops the previous machine's queue when switching to one without an update", async () => {
    vi.mocked(api.getActiveUpdate).mockImplementation(async (runtimeId) =>
      runtimeId === "runtime-1" ? update({}) : null,
    );
    const view = renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });
    expect(await screen.findByText("Updates when idle")).toBeInTheDocument();

    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <UpdateSection runtimeId="runtime-2" currentVersion="v0.4.0" isOnline />
      </I18nProvider>,
    );

    expect(await screen.findByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(screen.queryByText("Updates when idle")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
  });

  it("does not report a downgrade as done before the daemon runs the target", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(
      update({ status: "running", target_version: "v0.4.0", cancellable: false }),
    );

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.1" });

    expect(await screen.findByText("Updating...")).toBeInTheDocument();
    expect(screen.queryByText(/Update complete/)).not.toBeInTheDocument();
  });

  it("shows the machine's existing queue when another tab already queued one", async () => {
    vi.mocked(api.initiateUpdate).mockRejectedValue(new ApiError("in progress", 409, "Conflict"));
    vi.mocked(api.getActiveUpdate).mockResolvedValueOnce(null).mockResolvedValueOnce(update({}));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });
    fireEvent.click(await screen.findByRole("button", { name: "Update" }));

    expect(await screen.findByText("Updates when idle")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.queryByText("Failed to initiate update")).not.toBeInTheDocument();
  });

  it("does not claim installation started when the update already ended", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(update({}));
    vi.mocked(api.cancelUpdate).mockRejectedValue(new ApiError("not cancellable", 409, "Conflict"));
    vi.mocked(api.getUpdateResult).mockResolvedValue(update({ status: "expired", cancellable: false }));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(
      await screen.findByText("Expired after waiting 24 hours for the machine to go idle."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/installation has already started/)).not.toBeInTheDocument();
  });

  it("shows an expired queue as a neutral note", async () => {
    vi.mocked(api.getActiveUpdate).mockResolvedValue(update({ status: "expired", cancellable: false }));

    renderSection({ runtimeId: "runtime-1", currentVersion: "v0.4.0" });

    expect(
      await screen.findByText("Expired after waiting 24 hours for the machine to go idle."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update" })).toBeInTheDocument();
  });
});
