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

function renderPanel(props: { onImported?: (skill: unknown) => void; onBulkDone?: () => void } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={queryClient}>
        <RuntimeLocalSkillImportPanel {...props} />
      </QueryClientProvider>
    </I18nWrapper>,
  );
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
    mockResolveRuntimeLocalSkillImport.mockResolvedValue({
      status: "created",
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
            supports_conflict: true,
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
      .mockResolvedValueOnce({ status: "created", skill: MOCK_IMPORTED_SKILL_A })
      .mockResolvedValueOnce({ status: "created", skill: MOCK_IMPORTED_SKILL_B });

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

    // Verify summary shows both as created
    expect(screen.getByText("Created")).toBeInTheDocument();
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
      .mockResolvedValueOnce({ status: "created", skill: MOCK_IMPORTED_SKILL_A })
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

    // Summary should show created and skipped
    expect(screen.getByText("Created")).toBeInTheDocument();
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
      .mockResolvedValueOnce({ status: "created", skill: MOCK_IMPORTED_SKILL_A })
      .mockResolvedValueOnce({ status: "created", skill: MOCK_IMPORTED_SKILL_B });

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

  it("resolves a creator conflict by overwriting the existing skill", async () => {
    mockResolveRuntimeLocalSkillImport
      .mockResolvedValueOnce({
        status: "conflict",
        conflict: {
          existing_skill_id: "existing-skill-1",
          existing_created_by: "user-1",
          can_overwrite: true,
        },
      })
      .mockResolvedValueOnce({
        status: "updated",
        skill: {
          ...MOCK_IMPORTED_SKILL_A,
          id: "existing-skill-1",
        },
      });

    renderPanel();

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Review Helper/i }));

    const importButton = screen.getByRole("button", {
      name: /Import to Workspace/i,
    });
    await waitFor(() => expect(importButton).not.toBeDisabled(), {
      timeout: 5000,
    });
    fireEvent.click(importButton);

    expect(
      await screen.findByText(/A skill with this name already exists/i),
    ).toBeInTheDocument();

    const applyButton = screen.getByRole("button", {
      name: /Apply decisions/i,
    });
    await waitFor(() => expect(applyButton).not.toBeDisabled(), {
      timeout: 5000,
    });
    fireEvent.click(applyButton);

    await waitFor(
      () => {
        expect(mockResolveRuntimeLocalSkillImport).toHaveBeenLastCalledWith(
          "runtime-1",
          {
            skill_key: "review-helper",
            name: "Review Helper",
            description: "Review pull requests",
            supports_conflict: true,
            action: "overwrite",
            target_skill_id: "existing-skill-1",
          },
        );
      },
      { timeout: 5000 },
    );

    expect(await screen.findByText("Updated")).toBeInTheDocument();
  });

  it("keeps bulk completion behavior when conflict resolution leaves one success", async () => {
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [MOCK_SKILL_A, MOCK_SKILL_B],
        }),
    });
    mockResolveRuntimeLocalSkillImport
      .mockResolvedValueOnce({
        status: "conflict",
        conflict: {
          existing_skill_id: "existing-skill-1",
          existing_created_by: "user-1",
          can_overwrite: true,
        },
      })
      .mockRejectedValueOnce(new Error("daemon failed"))
      .mockResolvedValueOnce({
        status: "updated",
        skill: {
          ...MOCK_IMPORTED_SKILL_A,
          id: "existing-skill-1",
        },
      });

    const onImported = vi.fn();
    const onBulkDone = vi.fn();
    renderPanel({ onImported, onBulkDone });

    expect(
      await screen.findByText("Review Helper", {}, { timeout: 5000 }),
    ).toBeInTheDocument();

    const selectAllLabel = screen.getByText(/Select all/i);
    const selectAllCheckbox = selectAllLabel
      .closest("label")!
      .querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox);

    const importButton = screen.getByRole("button", {
      name: /Import 2 Skills/i,
    });
    await waitFor(() => expect(importButton).not.toBeDisabled(), {
      timeout: 5000,
    });
    fireEvent.click(importButton);

    expect(
      await screen.findByText(/A skill with this name already exists/i),
    ).toBeInTheDocument();

    const applyButton = screen.getByRole("button", {
      name: /Apply decisions/i,
    });
    await waitFor(() => expect(applyButton).not.toBeDisabled(), {
      timeout: 5000,
    });
    fireEvent.click(applyButton);

    await waitFor(
      () => {
        expect(
          screen.getByRole("button", { name: /Done/i }),
        ).toBeInTheDocument();
      },
      { timeout: 10000 },
    );

    fireEvent.click(screen.getByRole("button", { name: /Done/i }));
    expect(onBulkDone).toHaveBeenCalledTimes(1);
    expect(onImported).not.toHaveBeenCalled();
  });
});
