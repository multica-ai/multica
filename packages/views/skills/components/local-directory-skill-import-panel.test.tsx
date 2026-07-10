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

const mockCreateSkill = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    createSkill: (...args: unknown[]) => mockCreateSkill(...args),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { LocalDirectorySkillImportPanel } from "./local-directory-skill-import-panel";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPanel(props: { onImported?: (skill: unknown) => void; onBulkDone?: () => void } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={queryClient}>
        <LocalDirectorySkillImportPanel {...props} />
      </QueryClientProvider>
    </I18nWrapper>,
  );
}

function fileAt(path: string, content: string): File {
  const file = new File([content], path.split("/").pop() ?? "file", {
    type: "text/plain",
  });
  Object.defineProperty(file, "webkitRelativePath", {
    value: path,
    configurable: true,
  });
  return file;
}

describe("LocalDirectorySkillImportPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateSkill.mockImplementation(async (payload: { name: string }) => ({
      id: `skill-${payload.name}`,
      workspace_id: "ws-1",
      name: payload.name,
      description: "",
      content: "",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-04-16T00:00:00Z",
      updated_at: "2026-04-16T00:00:00Z",
    }));
  });

  it("lists skills from a selected directory and imports the selected bundles", async () => {
    renderPanel();

    const reviewSkill = [
      "---",
      "name: Review Helper",
      "description: Review pull requests",
      "---",
      "",
      "# Review Helper",
    ].join("\n");
    const codeGenSkill = [
      "---",
      "name: Code Gen",
      "description: Generate code from specs",
      "---",
      "",
      "# Code Gen",
    ].join("\n");

    const input = screen.getByTestId("local-directory-input");
    fireEvent.change(input, {
      target: {
        files: [
          fileAt("skills/review-helper/SKILL.md", reviewSkill),
          fileAt("skills/review-helper/references/checklist.md", "Checklist"),
          fileAt("skills/code-gen/SKILL.md", codeGenSkill),
        ],
      },
    });

    expect(await screen.findByText("Review Helper")).toBeInTheDocument();
    expect(screen.getByText("Code Gen")).toBeInTheDocument();

    const selectAllLabel = screen.getByText(/Select all/i);
    const selectAllCheckbox = selectAllLabel
      .closest("label")!
      .querySelector("input[type='checkbox']")!;
    fireEvent.click(selectAllCheckbox);

    const importButton = screen.getByRole("button", {
      name: /Import 2 Skills/i,
    });
    fireEvent.click(importButton);

    await waitFor(() => {
      expect(mockCreateSkill).toHaveBeenCalledTimes(2);
    });

    expect(mockCreateSkill).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Review Helper",
        description: "Review pull requests",
        content: reviewSkill,
        files: [{ path: "references/checklist.md", content: "Checklist" }],
        config: {
          origin: {
            type: "local_directory",
            source_path: "review-helper",
          },
        },
      }),
    );
    expect(mockCreateSkill).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "Code Gen",
        description: "Generate code from specs",
        content: codeGenSkill,
        files: [],
        config: {
          origin: {
            type: "local_directory",
            source_path: "code-gen",
          },
        },
      }),
    );
  });
});
