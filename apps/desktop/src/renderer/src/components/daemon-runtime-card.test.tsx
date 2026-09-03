import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import type { DaemonStatus } from "../../../shared/daemon-types";

const translations = {
  desktop: {
    daemon: {
      view_logs: "查看日志",
      managed_externally: "由应用外部管理",
      start: "启动",
      restart: "重启",
      stop: "停止",
    },
  },
};

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
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resources: typeof translations) => string) =>
      selector(translations),
  }),
}));
vi.mock("./daemon-panel", () => ({ DaemonPanel: () => null }));
vi.mock("../platform/daemon-reauth", () => ({
  reauthenticateDaemon: vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { DaemonRuntimeActions } from "./daemon-runtime-card";

function stubDaemonAPI(status: DaemonStatus) {
  Object.defineProperty(window, "daemonAPI", {
    configurable: true,
    value: {
      getStatus: vi.fn().mockResolvedValue(status),
      onStatusChange: vi.fn(() => () => {}),
    },
  });
}

describe("DaemonRuntimeActions — externally managed daemon (#3916)", () => {
  it("hides Stop/Restart and shows the managed-outside hint for a daemon the app can't control", async () => {
    stubDaemonAPI({ state: "running", daemonId: "d1", externallyManaged: true });
    render(<DaemonRuntimeActions />);

    // The translated view-logs label still renders, confirming the running
    // branch mounted and uses the selected locale.
    expect(await screen.findByText("查看日志")).toBeInTheDocument();
    expect(screen.getByText("由应用外部管理")).toBeInTheDocument();
    expect(screen.queryByText("重启")).not.toBeInTheDocument();
    expect(screen.queryByText("停止")).not.toBeInTheDocument();
  });

  it("shows Stop/Restart for a normally-managed running daemon (no 误伤)", async () => {
    stubDaemonAPI({
      state: "running",
      daemonId: "d1",
      externallyManaged: false,
    });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText("重启")).toBeInTheDocument();
    expect(screen.getByText("停止")).toBeInTheDocument();
    expect(
      screen.queryByText("由应用外部管理"),
    ).not.toBeInTheDocument();
  });
});

describe("DaemonRuntimeActions — recovery budget", () => {
  it("offers a manual Start when automatic recovery is paused", async () => {
    stubDaemonAPI({ state: "recovery_paused" });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText("启动")).toBeInTheDocument();
  });
});
