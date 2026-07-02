import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AgentRuntime, ProjectResource } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectResourcesSection } from "./project-resources-section";

const mockCreateResourceMutateAsync = vi.fn();
let mockResources: ProjectResource[] = [];
let mockRuntimes: AgentRuntime[] = [];

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    if (options.queryKey?.[0] === "project-resources") {
      return { data: mockResources };
    }
    if (options.queryKey?.[0] === "runtimes") {
      return { data: mockRuntimes };
    }
    return { data: [] };
  },
}));

vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({
    queryKey: ["project-resources"],
    queryFn: vi.fn(),
  }),
  useCreateProjectResource: () => ({
    mutateAsync: mockCreateResourceMutateAsync,
    isPending: false,
  }),
  useDeleteProjectResource: () => ({ mutateAsync: vi.fn() }),
  useUpdateProjectResource: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [],
  }),
}));

vi.mock("../../platform", () => ({
  isDesktopShell: () => false,
  pickDirectory: vi.fn(),
  validateLocalDirectory: vi.fn(),
  useLocalDaemonStatus: () => ({
    running: false,
    daemonId: null,
    deviceName: null,
  }),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "workspace-1",
    daemon_id: "daemon-a",
    name: "ThinkPad",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "codex",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ProjectResourcesSection", () => {
  beforeEach(() => {
    mockCreateResourceMutateAsync.mockReset();
    mockResources = [];
    mockRuntimes = [];
  });

  it("attaches a manual local_directory path on web using the selected local runtime", async () => {
    const user = userEvent.setup();
    mockRuntimes = [makeRuntime()];

    renderWithI18n(<ProjectResourcesSection projectId="project-1" />);

    await user.type(
      screen.getByRole("textbox", { name: "Local directory path" }),
      "C:\\Users\\imshe\\multica_workspaces\\abc\\workdir",
    );
    await user.click(screen.getByRole("button", { name: "Add local directory" }));

    expect(mockCreateResourceMutateAsync).toHaveBeenCalledWith({
      resource_type: "local_directory",
      resource_ref: {
        local_path: "C:\\Users\\imshe\\multica_workspaces\\abc\\workdir",
        daemon_id: "daemon-a",
        label: "workdir",
      },
    });
  });
});
