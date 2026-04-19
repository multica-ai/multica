// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import type { RuntimeDevice } from "@multica/core/types";
import { TaskRuntimeBadge } from "./task-runtime-badge";

const baseRuntime: RuntimeDevice = {
  id: "rt-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "My Runtime",
  runtime_mode: "local",
  provider: "claude",
  status: "online",
  device_info: "macOS",
  metadata: {},
  owner_id: null,
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("TaskRuntimeBadge", () => {
  it("renders nothing when runtime is null", () => {
    const { container } = render(<TaskRuntimeBadge runtime={null} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders runtime name with a green dot when online", () => {
    render(<TaskRuntimeBadge runtime={baseRuntime} />);
    expect(screen.getByText("My Runtime")).toBeDefined();
    const badge = screen.getByTitle("Runtime: My Runtime (local)");
    // The status dot should have the success class for an online runtime
    const dot = badge.querySelector(".bg-success");
    expect(dot).toBeTruthy();
  });

  it("renders runtime name with a muted dot when offline", () => {
    const offlineRuntime: RuntimeDevice = { ...baseRuntime, status: "offline" };
    render(<TaskRuntimeBadge runtime={offlineRuntime} />);
    expect(screen.getByText("My Runtime")).toBeDefined();
    const badge = screen.getByTitle("Runtime: My Runtime (local)");
    const dot = badge.querySelector(".bg-muted-foreground\\/40");
    expect(dot).toBeTruthy();
  });
});
