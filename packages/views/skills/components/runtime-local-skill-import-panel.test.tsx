// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";

const TEST_RESOURCES = {
  en: { common: enCommon, skills: enSkills },
};

const mockResolveRuntimeLocalSkillImport = vi.hoisted(() => vi.fn());
const mockRuntimeListOptions = vi.hoisted(() => vi.fn());
const mockRuntimeLocalSkillsOptions = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => {
  const stateUser = { id: "user-1", email: "u@example.com", name: "User" };
  const useAuthStore = (selector?: (s: { user: typeof stateUser }) => unknown) => {
    const state = { user: stateUser };
    return selector ? selector(state) : state;
  };
  return { useAuthStore };
});

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

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { RuntimeLocalSkillImportPanel } from "./runtime-local-skill-import-panel";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={queryClient}>
        <RuntimeLocalSkillImportPanel />
      </QueryClientProvider>
    </I18nWrapper>,
  );
}

describe("RuntimeLocalSkillImportPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();

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

  it("imports a local skill from the selected runtime", async () => {
    renderPanel();

    // Five-step async cascade (runtime list → setSelectedRuntimeId effect →
    // skills query → auto-select effect → row render). Fast locally, slow on
    // CI — bump timeouts above RTL's 1 s default so the jsdom/Vitest work
    // queue actually has time to drain.
    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    const importButton = screen.getByRole("button", {
      name: /Import to Workspace/i,
    });
    await waitFor(
      () => {
        expect(importButton).not.toBeDisabled();
      },
      { timeout: 5000 },
    );
    fireEvent.click(importButton);

    await waitFor(
      () => {
        expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledWith(
          "runtime-1",
          {
            skill_key: "review-helper",
            name: "Review Helper",
            description: "Review pull requests",
          },
        );
      },
      { timeout: 5000 },
    );
  });
});
