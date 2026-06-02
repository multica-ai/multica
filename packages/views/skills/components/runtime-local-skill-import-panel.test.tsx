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
const mockSkillListOptions = vi.hoisted(() => vi.fn());

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

vi.mock("@multica/core/workspace/queries", () => ({
  skillListOptions: (...args: unknown[]) => mockSkillListOptions(...args),
  skillDetailOptions: (_wsId: string, skillId: string) => ({
    queryKey: ["workspaces", "ws-1", "skills", skillId],
  }),
  workspaceKeys: {
    skills: (wsId: string) => ["workspaces", wsId, "skills"],
    agents: (wsId: string) => ["workspaces", wsId, "agents"],
  },
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

const MOCK_RUNTIME = {
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
};

const MOCK_SKILL_A = {
  key: "review-helper",
  name: "Review Helper",
  description: "Review pull requests",
  provider: "claude",
  source_path: "~/.claude/skills/review-helper",
  file_count: 2,
};

const MOCK_SKILL_B = {
  key: "code-gen",
  name: "Code Gen",
  description: "Generate code from specs",
  provider: "claude",
  source_path: "~/.claude/skills/code-gen",
  file_count: 3,
};

const MOCK_IMPORTED_SKILL_A = {
  id: "skill-1",
  workspace_id: "ws-1",
  name: "Review Helper",
  description: "Review pull requests",
  content: "# Review Helper",
  config: {},
  files: [],
  created_by: "user-1",
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
};

const MOCK_IMPORTED_SKILL_B = {
  id: "skill-2",
  workspace_id: "ws-1",
  name: "Code Gen",
  description: "Generate code from specs",
  content: "# Code Gen",
  config: {},
  files: [],
  created_by: "user-1",
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
};

const MOCK_WORKSPACE_SKILL_A = {
  id: "existing-skill-1",
  workspace_id: "ws-1",
  name: "Review Helper",
  description: "Existing review helper",
  config: {},
  created_by: "user-1",
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
};

const MOCK_WORKSPACE_SKILL_B = {
  id: "existing-skill-2",
  workspace_id: "ws-1",
  name: "Code Gen",
  description: "Existing code generator",
  config: {},
  created_by: "user-1",
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
};

function renderPanel(props: { onImported?: (skill: unknown) => void; onBulkDone?: () => void } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = render(
    <I18nWrapper>
      <QueryClientProvider client={queryClient}>
        <RuntimeLocalSkillImportPanel {...props} />
      </QueryClientProvider>
    </I18nWrapper>,
  );
  return { ...result, queryClient };
}

describe("RuntimeLocalSkillImportPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mockRuntimeListOptions.mockReturnValue({
      queryKey: ["runtimes", "ws-1", "list"],
      queryFn: () => Promise.resolve([MOCK_RUNTIME]),
    });
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A],
        }),
    });
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () => Promise.resolve([]),
    });
    mockResolveRuntimeLocalSkillImport.mockResolvedValue({
      skill: MOCK_IMPORTED_SKILL_A,
    });
  });

  it("imports a single skill when selected via checkbox", async () => {
    renderPanel();

    // Wait for skill list to render
    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    // Click the skill row to toggle its checkbox
    const skillButton = screen.getByRole("button", { name: /Review Helper/i });
    fireEvent.click(skillButton);

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

  it("imports multiple skills in sequence and shows summary", async () => {
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A, MOCK_SKILL_B],
        }),
    });
    mockResolveRuntimeLocalSkillImport
      .mockResolvedValueOnce({ skill: MOCK_IMPORTED_SKILL_A })
      .mockResolvedValueOnce({ skill: MOCK_IMPORTED_SKILL_B });

    renderPanel();

    // Wait for skills to render
    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Code Gen")).toBeInTheDocument();

    // Click select all checkbox (the native one in the label)
    const selectAllLabel = screen.getByText(/Select all/i);
    const selectAllCheckbox = selectAllLabel.closest("label")!.querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox);

    // Button should now say "Import 2 Skills"
    const importButton = screen.getByRole("button", {
      name: /Import 2 Skills/i,
    });
    await waitFor(
      () => {
        expect(importButton).not.toBeDisabled();
      },
      { timeout: 5000 },
    );
    fireEvent.click(importButton);

    // Wait for completion — summary should appear with "Done" button
    await waitFor(
      () => {
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );

    expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledTimes(2);

    // Verify summary shows both as imported
    expect(screen.getByText("Imported")).toBeInTheDocument();
  });

  it("handles partial failures gracefully", async () => {
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A, MOCK_SKILL_B],
        }),
    });
    mockResolveRuntimeLocalSkillImport
      .mockResolvedValueOnce({ skill: MOCK_IMPORTED_SKILL_A })
      .mockRejectedValueOnce(new Error("409 conflict: already exists"));

    renderPanel();

    // Wait for skills
    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    // Select all
    const selectAllLabel2 = screen.getByText(/Select all/i);
    const selectAllCheckbox2 = selectAllLabel2.closest("label")!.querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox2);

    // Import
    const importButton = screen.getByRole("button", {
      name: /Import 2 Skills/i,
    });
    await waitFor(
      () => {
        expect(importButton).not.toBeDisabled();
      },
      { timeout: 5000 },
    );
    fireEvent.click(importButton);

    // Wait for Done
    await waitFor(
      () => {
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );

    // Summary should show imported and skipped
    expect(screen.getByText("Imported")).toBeInTheDocument();
    expect(screen.getByText("Skipped")).toBeInTheDocument();
  });

  it("calls onImported when exactly one skill succeeds", async () => {
    const onImported = vi.fn();
    renderPanel({ onImported });

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    // Select the single skill
    const skillButton = screen.getByRole("button", { name: /Review Helper/i });
    fireEvent.click(skillButton);

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

    // Wait for Done button
    await waitFor(
      () => {
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );

    // Click Done — should call onImported with the single skill
    fireEvent.click(screen.getByRole("button", { name: /Done/i }));
    expect(onImported).toHaveBeenCalledWith(MOCK_IMPORTED_SKILL_A);
  });

  it("seeds imported skill detail cache with supporting files", async () => {
    const importedWithFiles = {
      ...MOCK_IMPORTED_SKILL_A,
      files: [
        {
          id: "file-1",
          skill_id: "skill-1",
          path: "templates/check.md",
          content: "checklist",
          created_at: "2026-04-16T00:00:00Z",
          updated_at: "2026-04-16T00:00:00Z",
        },
      ],
    };
    mockResolveRuntimeLocalSkillImport.mockResolvedValueOnce({
      skill: importedWithFiles,
    });

    const { queryClient } = renderPanel();

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Review Helper/i }));
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
        expect(queryClient.getQueryData(["workspaces", "ws-1", "skills", "skill-1"]))
          .toEqual(importedWithFiles);
      },
      { timeout: 10000 },
    );
  });

  it("calls onBulkDone when multiple skills succeed", async () => {
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A, MOCK_SKILL_B],
        }),
    });
    mockResolveRuntimeLocalSkillImport
      .mockResolvedValueOnce({ skill: MOCK_IMPORTED_SKILL_A })
      .mockResolvedValueOnce({ skill: MOCK_IMPORTED_SKILL_B });

    const onImported = vi.fn();
    const onBulkDone = vi.fn();
    renderPanel({ onImported, onBulkDone });

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    // Select all
    const selectAllLabel3 = screen.getByText(/Select all/i);
    const selectAllCheckbox3 = selectAllLabel3.closest("label")!.querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox3);

    const importButton = screen.getByRole("button", {
      name: /Import 2 Skills/i,
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
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );

    // Click Done — should call onBulkDone, NOT onImported
    fireEvent.click(screen.getByRole("button", { name: /Done/i }));
    expect(onBulkDone).toHaveBeenCalledTimes(1);
    expect(onImported).not.toHaveBeenCalled();
  });

  it("prompts before importing conflicting skills and sends overwrite only for checked conflicts", async () => {
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A, MOCK_SKILL_B],
        }),
    });
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () =>
        Promise.resolve([MOCK_WORKSPACE_SKILL_A, MOCK_WORKSPACE_SKILL_B]),
    });
    mockResolveRuntimeLocalSkillImport.mockResolvedValueOnce({
      skill: MOCK_IMPORTED_SKILL_A,
    });

    renderPanel();

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    const selectAllLabel = screen.getByText(/Select all/i);
    const selectAllCheckbox = selectAllLabel
      .closest("label")!
      .querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox);

    fireEvent.click(screen.getByRole("button", { name: /Import 2 Skills/i }));

    expect(
      await screen.findByText("Skill name conflicts", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    const reviewHelperCheckbox = screen
      .getAllByText("Review Helper")
      .at(-1)!
      .closest("label")!
      .querySelector("[data-slot='checkbox']")!;
    fireEvent.click(reviewHelperCheckbox);

    fireEvent.click(screen.getByRole("button", { name: /Continue import/i }));

    await waitFor(
      () => {
        expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledTimes(1);
      },
      { timeout: 5000 },
    );
    expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledWith(
      "runtime-1",
      {
        skill_key: "review-helper",
        name: "Review Helper",
        description: "Review pull requests",
        overwrite: true,
      },
    );

    await waitFor(
      () => {
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );
    expect(screen.getByText("Skipped")).toBeInTheDocument();
  });

  it("does not prompt for case-only name differences or send overwrite", async () => {
    const caseDifferentSkill = {
      ...MOCK_SKILL_A,
      name: "review helper",
    };
    const caseDifferentImportedSkill = {
      ...MOCK_IMPORTED_SKILL_A,
      name: "review helper",
    };
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [caseDifferentSkill],
        }),
    });
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () => Promise.resolve([MOCK_WORKSPACE_SKILL_A]),
    });
    mockResolveRuntimeLocalSkillImport.mockResolvedValueOnce({
      skill: caseDifferentImportedSkill,
    });

    renderPanel();

    expect(
      await screen.findByText("review helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    const skillButton = screen.getByRole("button", { name: /review helper/i });
    fireEvent.click(skillButton);

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
        expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledTimes(1);
      },
      { timeout: 5000 },
    );
    expect(screen.queryByText("Skill name conflicts")).not.toBeInTheDocument();
    expect(mockResolveRuntimeLocalSkillImport).toHaveBeenCalledWith(
      "runtime-1",
      {
        skill_key: "review-helper",
        name: "review helper",
        description: "Review pull requests",
      },
    );
  });
});
