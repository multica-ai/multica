import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import type { DaemonStatus } from "../../../shared/daemon-types";

// The component only needs these to render; stub them so the test focuses on
// the externally-managed branching, not data fetching.
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));
vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["snapshot"] }),
}));
vi.mock("./daemon-panel", () => ({ DaemonPanel: () => null }));
vi.mock("../platform/daemon-reauth", () => ({
  reauthenticateDaemon: vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const runtimesTranslations = {
  drainAction: "Safe shutdown",
  drainCancel: "Cancel shutdown",
  drainForceStop: "Shut down now",
  stateDraining: "Draining",
  drainInlineHint: "{{inflight}} running · {{queued}} queued",
  drainConfirmTitle: "This will interrupt {{count}} running task(s).",
  drainQueuedHint: "{{count}} queued task(s) will remain until they time out.",
  drainDescription:
    "No new tasks will be accepted; the daemon shuts down once running tasks finish.",
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (
      selector: (resources: typeof runtimesTranslations) => string,
      values?: Record<string, unknown>,
    ) => {
      const template = selector(runtimesTranslations);
      return Object.entries(values ?? {}).reduce(
        (result, [key, value]) => result.replace(`{{${key}}}`, String(value)),
        template,
      );
    },
  }),
}));

import { DaemonRuntimeActions } from "./daemon-runtime-card";

function stubDaemonAPI(status: DaemonStatus, drain = vi.fn()) {
  Object.defineProperty(window, "daemonAPI", {
    configurable: true,
    value: {
      getStatus: vi.fn().mockResolvedValue(status),
      onStatusChange: vi.fn(() => () => {}),
      drain,
    },
  });
}

describe("DaemonRuntimeActions — externally managed daemon (#3916)", () => {
  it("hides Stop/Restart and shows the managed-outside hint for a daemon the app can't control", async () => {
    stubDaemonAPI({ state: "running", daemonId: "d1", externallyManaged: true });
    render(<DaemonRuntimeActions />);

    // View logs still renders, confirming the running branch mounted.
    expect(await screen.findByText("View logs")).toBeInTheDocument();
    expect(screen.getByText("Managed outside the app")).toBeInTheDocument();
    expect(screen.queryByText("Restart")).not.toBeInTheDocument();
    expect(screen.queryByText("Stop")).not.toBeInTheDocument();
    // Drain is a lifecycle control the app can't drive either — hidden too.
    expect(screen.queryByText("Safe shutdown")).not.toBeInTheDocument();
  });

  it("shows Stop/Restart for a normally-managed running daemon (no 误伤)", async () => {
    stubDaemonAPI({
      state: "running",
      daemonId: "d1",
      externallyManaged: false,
    });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText("Restart")).toBeInTheDocument();
    expect(screen.getByText("Stop")).toBeInTheDocument();
    expect(
      screen.queryByText("Managed outside the app"),
    ).not.toBeInTheDocument();
  });
});

describe("DaemonRuntimeActions — draining state (NEX-38)", () => {
  it("shows the draining badge, cancel, and force-stop buttons", async () => {
    stubDaemonAPI({ state: "draining", daemonId: "d1" });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText("Draining")).toBeInTheDocument();
    expect(screen.getByText("0 running · 0 queued")).toBeInTheDocument();
    expect(screen.getByText("Cancel shutdown")).toBeInTheDocument();
    expect(screen.getByText("Shut down now")).toBeInTheDocument();
    // No start / stop / restart in the draining branch.
    expect(screen.queryByText("Stop")).not.toBeInTheDocument();
    expect(screen.queryByText("Restart")).not.toBeInTheDocument();
    expect(screen.queryByText("Start")).not.toBeInTheDocument();
  });

  it("calls daemon drain with `abort` when cancel is clicked", async () => {
    const drain = vi.fn().mockResolvedValue({ success: true });
    stubDaemonAPI({ state: "draining", daemonId: "d1" }, drain);
    render(<DaemonRuntimeActions />);

    fireEvent.click(await screen.findByText("Cancel shutdown"));
    expect(drain).toHaveBeenCalledWith("abort");
  });

  it("asks for confirmation before force-stop, then drains with `finish_then_stop`", async () => {
    const drain = vi.fn().mockResolvedValue({ success: true });
    stubDaemonAPI({ state: "draining", daemonId: "d1" }, drain);
    render(<DaemonRuntimeActions />);

    fireEvent.click(await screen.findByText("Shut down now"));
    expect(
      await screen.findByText(
        "This will interrupt 0 running task(s).",
      ),
    ).toBeInTheDocument();
    // The dialog confirm is labelled "Shut down now" too — two elements now.
    fireEvent.click(screen.getAllByText("Shut down now")[1]!);
    await waitFor(() => expect(drain).toHaveBeenCalledWith("finish_then_stop"));
  });

  it("starts safe shutdown from the running state via `drain`", async () => {
    const drain = vi.fn().mockResolvedValue({ success: true });
    stubDaemonAPI(
      { state: "running", daemonId: "d1", externallyManaged: false },
      drain,
    );
    render(<DaemonRuntimeActions />);

    fireEvent.click(await screen.findByText("Safe shutdown"));
    await waitFor(() => expect(drain).toHaveBeenCalledWith("drain"));
  });
});

