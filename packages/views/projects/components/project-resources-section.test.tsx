import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enProjects from "../../locales/en/projects.json";

const TEST_RESOURCES = { en: { common: enCommon, projects: enProjects } };

const mocks = vi.hoisted(() => ({
  createResource: vi.fn(),
  updateResource: vi.fn(),
  deleteResource: vi.fn(),
  desktopMode: false,
  daemonStatus: {
    daemonId: null as string | null,
    deviceName: null as string | null,
    running: false,
  },
  runtimes: [] as Array<Record<string, unknown>>,
  pickDirectory: vi.fn(),
  validateLocalDirectory: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: {
    error: mocks.toastError,
    success: mocks.toastSuccess,
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ repos: [] }),
}));

vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({
    queryKey: ["project-resources", "ws-1", "project-1"],
    queryFn: () => Promise.resolve([]),
  }),
  useCreateProjectResource: () => ({
    mutateAsync: mocks.createResource,
    isPending: false,
  }),
  useUpdateProjectResource: () => ({
    mutateAsync: mocks.updateResource,
    isPending: false,
  }),
  useDeleteProjectResource: () => ({
    mutateAsync: mocks.deleteResource,
    isPending: false,
  }),
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimes", "ws-1"],
    queryFn: () => Promise.resolve(mocks.runtimes),
  }),
}));

vi.mock("../../platform", () => ({
  isDesktopShell: () => mocks.desktopMode,
  pickDirectory: mocks.pickDirectory,
  useLocalDaemonStatus: () => mocks.daemonStatus,
  validateLocalDirectory: mocks.validateLocalDirectory,
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

import { ProjectResourcesSection } from "./project-resources-section";

function makeRuntime(overrides: Record<string, unknown> = {}) {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-web",
    name: "Codex (Jay Mac)",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "",
    status: "online",
    device_info: "Jay Mac · macOS",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: "2026-06-23T00:00:00Z",
    created_at: "2026-06-23T00:00:00Z",
    updated_at: "2026-06-23T00:00:00Z",
    ...overrides,
  };
}

function renderSection() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ProjectResourcesSection projectId="project-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { ...view, qc };
}

describe("ProjectResourcesSection", () => {
  beforeEach(() => {
    mocks.createResource.mockReset();
    mocks.updateResource.mockReset();
    mocks.deleteResource.mockReset();
    mocks.desktopMode = false;
    mocks.daemonStatus = {
      daemonId: null,
      deviceName: null,
      running: false,
    };
    mocks.runtimes = [];
    mocks.pickDirectory.mockReset();
    mocks.validateLocalDirectory.mockReset();
    mocks.toastError.mockReset();
    mocks.toastSuccess.mockReset();
  });

  it("attaches a local directory from the native desktop folder picker", async () => {
    mocks.desktopMode = true;
    mocks.daemonStatus = {
      daemonId: "daemon-desktop",
      deviceName: "Jay Mac",
      running: true,
    };
    mocks.pickDirectory.mockResolvedValue({
      ok: true,
      path: "/Users/jay/work/repo",
      basename: "repo",
    });
    mocks.validateLocalDirectory.mockResolvedValue({ ok: true });
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Add local directory" }));

    await waitFor(() => {
      expect(mocks.createResource).toHaveBeenCalledWith({
        resource_type: "local_directory",
        resource_ref: {
          local_path: "/Users/jay/work/repo",
          daemon_id: "daemon-desktop",
          label: "repo",
        },
      });
    });
    expect(mocks.pickDirectory).toHaveBeenCalledTimes(1);
    expect(mocks.validateLocalDirectory).toHaveBeenCalledWith(
      "/Users/jay/work/repo",
    );
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Local directory attached");
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("attaches a manually entered local directory on web", async () => {
    mocks.runtimes = [makeRuntime()];
    const { qc } = renderSection();
    await waitFor(() => {
      expect(qc.getQueryData(["runtimes", "ws-1"])).toEqual(mocks.runtimes);
    });

    const localPathInput = screen.getByPlaceholderText("Absolute local path");
    fireEvent.change(localPathInput, {
      target: { value: "/Users/jay/work/repo" },
    });
    fireEvent.submit(localPathInput.closest("form")!);

    await waitFor(() => {
      expect(mocks.createResource).toHaveBeenCalledWith({
        resource_type: "local_directory",
        resource_ref: {
          local_path: "/Users/jay/work/repo",
          daemon_id: "daemon-web",
          label: "repo",
        },
      });
    });
    expect(mocks.pickDirectory).not.toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Local directory attached");
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("does not attach local paths from the GitHub resource form", async () => {
    renderSection();

    const repoInput = screen.getByPlaceholderText("GitHub URL");
    fireEvent.change(repoInput, {
      target: { value: "/Users/jay/work/repo" },
    });
    fireEvent.submit(repoInput.closest("form")!);

    await waitFor(() => {
      expect(mocks.toastError).toHaveBeenCalledWith(
        "Use Add local directory to attach a local path.",
      );
    });
    expect(mocks.createResource).not.toHaveBeenCalled();
    expect(repoInput).toHaveValue("/Users/jay/work/repo");
  });
});
