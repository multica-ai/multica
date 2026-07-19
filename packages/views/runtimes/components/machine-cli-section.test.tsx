// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import type { RuntimeMachine } from "./runtime-machines";

const mockUpdateSection = vi.hoisted(() => vi.fn());

vi.mock("./update-section", () => ({
  UpdateSection: (props: Record<string, unknown>) => {
    mockUpdateSection(props);
    return props.runtimeId ? (
      <button type="button">Update</button>
    ) : (
      <span>CLI status</span>
    );
  },
}));

import { MachineCliSection } from "./machine-cli-section";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (dev.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: { cli_version: "0.3.17" },
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-07-15T00:00:00Z",
    created_at: "2026-07-15T00:00:00Z",
    updated_at: "2026-07-15T00:00:00Z",
    ...overrides,
  };
}

function machine(runtimes: AgentRuntime[]): RuntimeMachine {
  const launchedBy = runtimes
    .filter((item) => item.status === "online")
    .map((item) => item.metadata?.launched_by)
    .find((value): value is string => typeof value === "string");

  return {
    id: "local:daemon-1",
    daemonId: "daemon-1",
    title: "dev.local",
    subtitle: null,
    deviceInfo: "dev.local",
    cliVersion: "0.3.17",
    launchedBy: launchedBy ?? null,
    mode: "local",
    section: "remote",
    isCurrent: false,
    health: "online",
    runtimes,
    onlineCount: runtimes.filter((item) => item.status === "online").length,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: runtimes.map((item) => item.provider),
    lastSeenAt: "2026-07-15T00:00:00Z",
  };
}

describe("MachineCliSection", () => {
  beforeEach(() => {
    mockUpdateSection.mockClear();
  });

  it("renders one update control using an online viewer-owned runtime", () => {
    const offlineOwned = runtime({ id: "offline-owned", status: "offline" });
    const onlineOwned = runtime({
      id: "online-owned",
      metadata: {
        cli_version: "0.3.17",
        launched_by: "desktop",
      },
    });
    const otherUsers = runtime({ id: "other-user", owner_id: "user-2" });

    render(
      <MachineCliSection
        machine={machine([offlineOwned, onlineOwned, otherUsers])}
        currentUserId="user-1"
        canManageAnyRuntime={false}
      />,
    );

    expect(screen.getAllByRole("button", { name: "Update" })).toHaveLength(1);
    expect(mockUpdateSection).toHaveBeenCalledTimes(1);
    expect(mockUpdateSection).toHaveBeenCalledWith({
      runtimeId: "online-owned",
      currentVersion: "0.3.17",
      isOnline: true,
      launchedBy: "desktop",
    });
  });

  it("lets a workspace admin update through an online teammate-owned runtime", () => {
    const offline = runtime({
      id: "offline-other-user",
      owner_id: "user-2",
      status: "offline",
    });
    const online = runtime({
      id: "online-other-user",
      owner_id: "user-2",
    });

    render(
      <MachineCliSection
        machine={machine([offline, online])}
        currentUserId="user-1"
        canManageAnyRuntime
      />,
    );

    expect(screen.getAllByRole("button", { name: "Update" })).toHaveLength(1);
    expect(mockUpdateSection).toHaveBeenCalledWith({
      runtimeId: "online-other-user",
      currentVersion: "0.3.17",
      isOnline: true,
      launchedBy: null,
    });
  });

  it("shows read-only machine CLI status when the viewer owns no runtime", () => {
    render(
      <MachineCliSection
        machine={machine([
          runtime({
            owner_id: "user-2",
            metadata: {
              cli_version: "0.3.17",
              launched_by: "desktop",
            },
          }),
        ])}
        currentUserId="user-1"
        canManageAnyRuntime={false}
      />,
    );

    expect(screen.getByText("CLI status")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Update" }),
    ).not.toBeInTheDocument();
    expect(mockUpdateSection).toHaveBeenCalledWith({
      runtimeId: null,
      currentVersion: "0.3.17",
      isOnline: false,
      launchedBy: "desktop",
    });
  });
});
