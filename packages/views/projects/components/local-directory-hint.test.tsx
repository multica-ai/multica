import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ProjectResource } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enProjects from "../../locales/en/projects.json";

const TEST_RESOURCES = { en: { common: enCommon, projects: enProjects } };

const mockDaemonStatus = vi.hoisted(() => ({
  daemonId: null as string | null,
  deviceName: null as string | null,
  running: false,
}));

vi.mock("../../platform", () => ({
  useLocalDaemonStatus: () => mockDaemonStatus,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockListResources = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listProjectResources: (...args: unknown[]) => mockListResources(...args),
  },
}));

import { LocalDirectoryHint } from "./local-directory-hint";

function renderHint(projectId: string | null | undefined) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <LocalDirectoryHint projectId={projectId} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function makeLocalDirectoryResource(overrides: {
  daemon_id: string;
  local_path: string;
  label?: string;
}): ProjectResource {
  return {
    id: `res-${overrides.local_path}`,
    project_id: "proj-1",
    workspace_id: "ws-1",
    resource_type: "local_directory",
    resource_ref: {
      daemon_id: overrides.daemon_id,
      local_path: overrides.local_path,
      ...(overrides.label ? { label: overrides.label } : {}),
    },
    label: null,
    position: 0,
    created_at: new Date(0).toISOString(),
    created_by: null,
  };
}

describe("LocalDirectoryHint", () => {
  beforeEach(() => {
    mockDaemonStatus.daemonId = null;
    mockDaemonStatus.deviceName = null;
    mockDaemonStatus.running = false;
    mockListResources.mockReset();
  });

  it("renders nothing when project_id is null", () => {
    const { container } = renderHint(null);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when there's no local daemon", () => {
    mockListResources.mockResolvedValue({
      resources: [
        makeLocalDirectoryResource({
          daemon_id: "daemon-A",
          local_path: "/Users/foo/work",
          label: "work",
        }),
      ],
      total: 1,
    });
    const { container } = renderHint("proj-1");
    expect(container.firstChild).toBeNull();
  });

  it("renders the hint when a local_directory resource matches this daemon", async () => {
    mockDaemonStatus.daemonId = "daemon-A";
    mockDaemonStatus.running = true;
    mockListResources.mockResolvedValue({
      resources: [
        makeLocalDirectoryResource({
          daemon_id: "daemon-A",
          local_path: "/Users/foo/work",
          label: "work",
        }),
      ],
      total: 1,
    });
    renderHint("proj-1");
    await waitFor(() => {
      expect(screen.getByText("work")).toBeInTheDocument();
    });
    expect(screen.getByText(/Users\/foo\/work/)).toBeInTheDocument();
  });

  it("ignores resources pinned to a different daemon", async () => {
    mockDaemonStatus.daemonId = "daemon-A";
    mockDaemonStatus.running = true;
    mockListResources.mockResolvedValue({
      resources: [
        makeLocalDirectoryResource({
          daemon_id: "daemon-B",
          local_path: "/Users/foo/other-machine",
          label: "elsewhere",
        }),
      ],
      total: 1,
    });
    const { container } = renderHint("proj-1");
    // Allow the query to settle; the hint should still render nothing.
    await Promise.resolve();
    await Promise.resolve();
    expect(container.querySelector("div[class*='rounded-md']")).toBeNull();
  });
});
