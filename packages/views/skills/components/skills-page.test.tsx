// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const mockListSkills = vi.hoisted(() => vi.fn());
const mockResolveRuntimeLocalSkillImport = vi.hoisted(() => vi.fn());
const mockRuntimeListOptions = vi.hoisted(() => vi.fn());
const mockRuntimeLocalSkillsOptions = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    createSkill: vi.fn(),
    importSkill: vi.fn(),
    updateSkill: vi.fn(),
    deleteSkill: vi.fn(),
    getSkill: vi.fn(),
  },
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: (...args: unknown[]) => mockRuntimeListOptions(...args),
  runtimeLocalSkillsOptions: (...args: unknown[]) =>
    mockRuntimeLocalSkillsOptions(...args),
  runtimeLocalSkillsKeys: {
    forRuntime: (runtimeId: string) => ["runtimes", "local-skills", runtimeId],
  },
  resolveRuntimeLocalSkillImport: (...args: unknown[]) =>
    mockResolveRuntimeLocalSkillImport(...args),
}));

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({
    defaultLayout: undefined,
    onLayoutChanged: vi.fn(),
  }),
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: any) => <div>{children}</div>,
  ResizablePanel: ({ children }: any) => <div>{children}</div>,
  ResizableHandle: () => <div data-testid="resize-handle" />,
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import SkillsPage from "./skills-page";

function renderSkillsPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SkillsPage />
    </QueryClientProvider>,
  );
}

describe("SkillsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mockListSkills.mockResolvedValue([]);
    mockRuntimeListOptions.mockReturnValue({
      queryKey: ["runtimes", "ws-1", "list"],
      queryFn: () =>
        Promise.resolve([
          {
            id: "runtime-1",
            workspace_id: "ws-1",
            daemon_id: "daemon-1",
            name: "Claude (MacBook)",
            runtime_mode: "local",
            provider: "claude",
            launch_header: "",
            status: "online",
            device_info: "",
            metadata: {},
            owner_id: "user-1",
            last_seen_at: null,
            created_at: "2026-04-16T00:00:00Z",
            updated_at: "2026-04-16T00:00:00Z",
          },
        ]),
    });
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [
            {
              key: "review-helper",
              name: "Review Helper",
              description: "Review pull requests",
              provider: "claude",
              source_path: "~/.claude/skills/review-helper",
              file_count: 2,
            },
          ],
        }),
    });
    mockResolveRuntimeLocalSkillImport.mockResolvedValue({
      skill: {
        id: "skill-2",
        workspace_id: "ws-1",
        name: "Review Helper",
        description: "Review pull requests",
        content: "# Review Helper",
        config: {},
        files: [],
        created_by: "user-1",
        created_at: "2026-04-16T00:00:00Z",
        updated_at: "2026-04-16T00:00:00Z",
      },
    });
  });

  it("opens the runtime import dialog and imports a local skill", async () => {
    renderSkillsPage();

    const importButtons = await screen.findAllByRole("button", {
      name: /Import From Runtime/i,
    });
    fireEvent.click(importButtons[0]!);

    expect(await screen.findByText("Import Runtime Skill")).toBeInTheDocument();
    expect(await screen.findByText("Review Helper")).toBeInTheDocument();

    const importButton = screen.getByRole("button", {
      name: /Import to Workspace/i,
    });
    await waitFor(() => {
      expect(importButton).not.toBeDisabled();
    });
    fireEvent.click(importButton);

    await waitFor(() => {
      expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledWith("runtime-1", {
        skill_key: "review-helper",
        name: "Review Helper",
        description: "Review pull requests",
      });
    });
  });
});
