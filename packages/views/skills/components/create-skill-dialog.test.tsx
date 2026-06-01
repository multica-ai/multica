// @vitest-environment jsdom

import type { ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";
import { CreateSkillDialog } from "./create-skill-dialog";

const TEST_RESOURCES = {
  en: { common: enCommon, skills: enSkills },
};

const mockOpenExternal = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockSkillListOptions = vi.hoisted(() => vi.fn());

vi.mock("../../platform", () => ({
  openExternal: mockOpenExternal,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
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
    success: mockToastSuccess,
    error: vi.fn(),
  },
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderDialog(props: { onClose?: () => void; onCreated?: (skill: unknown) => void } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <I18nWrapper>
      <QueryClientProvider client={queryClient}>
        <CreateSkillDialog
          onClose={props.onClose ?? vi.fn()}
          onCreated={props.onCreated}
        />
      </QueryClientProvider>
    </I18nWrapper>,
  );
}

function makeDirectoryFile(
  body: string,
  name: string,
  webkitRelativePath: string,
): File {
  const file = new File([body], name, { type: "text/markdown" });
  Object.defineProperty(file, "webkitRelativePath", {
    configurable: true,
    value: webkitRelativePath,
  });
  return file;
}

describe("CreateSkillDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () => Promise.resolve([]),
    });
  });

  it("imports a local SKILL.md directory through the restored upload flow", async () => {
    const user = userEvent.setup();
    const onCreated = vi.fn();
    const importedSkill = {
      id: "skill-1",
      workspace_id: "ws-1",
      name: "local-review",
      description: "Review local changes",
      content: "---\nname: local-review\ndescription: Review local changes\n---\nbody",
      config: {},
      files: [
        {
          id: "file-1",
          skill_id: "skill-1",
          path: "guide.md",
          content: "guide",
          created_at: "2026-05-29T00:00:00Z",
          updated_at: "2026-05-29T00:00:00Z",
        },
      ],
      created_by: "user-1",
      created_at: "2026-05-29T00:00:00Z",
      updated_at: "2026-05-29T00:00:00Z",
    };
    const batchImportSkills = vi.fn().mockResolvedValue({
      created: [importedSkill],
      skipped: [],
    });

    setApiInstance({
      batchImportSkills,
    } as unknown as ApiClient);

    renderDialog({ onCreated });

    await user.click(screen.getByRole("button", { name: /Upload local directory/i }));

    const input = document.querySelector("input[type='file']") as HTMLInputElement | null;
    expect(input).not.toBeNull();

    fireEvent.change(input!, {
      target: {
        files: [
          makeDirectoryFile(
            "---\nname: local-review\ndescription: Review local changes\n---\nbody",
            "SKILL.md",
            "local-review/SKILL.md",
          ),
          makeDirectoryFile("guide", "guide.md", "local-review/guide.md"),
        ],
      },
    });

    expect(await screen.findByText("local-review")).toBeInTheDocument();
    expect(screen.getByText("1 file")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Import 1 Skill/i }));

    await waitFor(() => {
      expect(batchImportSkills).toHaveBeenCalledWith({
        skills: [
          {
            name: "local-review",
            description: "Review local changes",
            content: "---\nname: local-review\ndescription: Review local changes\n---\nbody",
            files: [{ path: "guide.md", content: "guide" }],
          },
        ],
      });
    });

    await user.click(await screen.findByRole("button", { name: "Done" }));

    expect(onCreated).toHaveBeenCalledWith(importedSkill);
  });

  it("prompts for local directory conflicts and sends overwrite only for checked skills", async () => {
    const user = userEvent.setup();
    const importedSkill = {
      id: "skill-1",
      workspace_id: "ws-1",
      name: "local-review",
      description: "Updated local review",
      content: "updated",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-05-29T00:00:00Z",
      updated_at: "2026-05-29T00:00:00Z",
    };
    const batchImportSkills = vi.fn().mockResolvedValue({
      created: [importedSkill],
      skipped: [],
    });
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () =>
        Promise.resolve([
          {
            id: "existing-skill",
            workspace_id: "ws-1",
            name: "local-review",
            description: "Existing local review",
            config: {},
            created_by: "user-1",
            created_at: "2026-05-29T00:00:00Z",
            updated_at: "2026-05-29T00:00:00Z",
          },
        ]),
    });

    setApiInstance({
      batchImportSkills,
    } as unknown as ApiClient);

    renderDialog();

    await user.click(screen.getByRole("button", { name: /Upload local directory/i }));

    const input = document.querySelector("input[type='file']") as HTMLInputElement | null;
    expect(input).not.toBeNull();

    fireEvent.change(input!, {
      target: {
        files: [
          makeDirectoryFile(
            "---\nname: local-review\ndescription: Updated local review\n---\nupdated",
            "SKILL.md",
            "local-review/SKILL.md",
          ),
        ],
      },
    });

    expect(await screen.findByText("local-review")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Import 1 Skill/i }));

    expect(await screen.findByText("Skill name conflicts")).toBeInTheDocument();
    expect(batchImportSkills).not.toHaveBeenCalled();

    const conflictCheckbox = screen
      .getAllByText("local-review")
      .at(-1)!
      .closest("label")!
      .querySelector("[data-slot='checkbox']")!;
    fireEvent.click(conflictCheckbox);

    await user.click(screen.getByRole("button", { name: /Continue import/i }));

    await waitFor(() => {
      expect(batchImportSkills).toHaveBeenCalledWith({
        skills: [
          {
            name: "local-review",
            description: "Updated local review",
            content: "---\nname: local-review\ndescription: Updated local review\n---\nupdated",
            files: [],
            overwrite: true,
          },
        ],
      });
    });
  });

  it("discovers multiple URL skills and imports the selected ones through per-skill URL import", async () => {
    const user = userEvent.setup();
    const importedSkill = {
      id: "skill-1",
      workspace_id: "ws-1",
      name: "foo",
      description: "Foo helper",
      content: "foo",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-05-29T00:00:00Z",
      updated_at: "2026-05-29T00:00:00Z",
    };
    const discoverImportSkills = vi.fn().mockResolvedValue({
      skills: [
        {
          name: "foo",
          description: "Foo helper",
          content: "---\nname: foo\n---\nfoo",
          config: { origin: { type: "gitee", path: "skills/foo" } },
          files: [],
          source_path: "skills/foo/SKILL.md",
          source_url: "https://gitee.com/acme/skills/tree/master/skills/foo",
        },
        {
          name: "bar",
          description: "Bar helper",
          content: "---\nname: bar\n---\nbar",
          config: { origin: { type: "gitee", path: "skills/bar" } },
          files: [],
          source_path: "skills/bar/SKILL.md",
          source_url: "https://gitee.com/acme/skills/tree/master/skills/bar",
        },
      ],
    });
    const importSkill = vi.fn().mockResolvedValue(importedSkill);
    const batchImportSkills = vi.fn();

    setApiInstance({
      discoverImportSkills,
      importSkill,
      batchImportSkills,
    } as unknown as ApiClient);

    renderDialog();

    await user.click(screen.getByRole("button", { name: /Import from URL/i }));
    await user.type(screen.getByLabelText("Skill URL"), "https://gitee.com/acme/skills");
    await user.click(screen.getByRole("button", { name: /^Import$/i }));

    expect(await screen.findByText("foo")).toBeInTheDocument();
    expect(screen.getByText("bar")).toBeInTheDocument();
    expect(discoverImportSkills).toHaveBeenCalledWith({
      url: "https://gitee.com/acme/skills",
    });

    await user.click(screen.getByRole("button", { name: /bar Bar helper/i }));

    await user.click(screen.getByRole("button", { name: /Import 1 Skill/i }));

    await waitFor(() => {
      expect(importSkill).toHaveBeenCalledWith({
        url: "https://gitee.com/acme/skills/tree/master/skills/foo",
        overwrite: undefined,
      });
    });
    expect(batchImportSkills).not.toHaveBeenCalled();
  });

  it("prompts for URL discovery conflicts and sends overwrite only for checked skills", async () => {
    const user = userEvent.setup();
    const importedSkill = {
      id: "skill-1",
      workspace_id: "ws-1",
      name: "Gitee Helper",
      description: "Updated from Gitee",
      content: "updated",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-05-29T00:00:00Z",
      updated_at: "2026-05-29T00:00:00Z",
    };
    const discoverImportSkills = vi.fn().mockResolvedValue({
      skills: [
        {
          name: "Gitee Helper",
          description: "Updated from Gitee",
          content: "updated",
          config: { origin: { type: "gitee", path: "helper" } },
          files: [],
          source_path: "helper/SKILL.md",
          source_url: "https://gitee.com/acme/helper/tree/master/helper",
        },
      ],
    });
    const importSkill = vi.fn().mockResolvedValue(importedSkill);
    const batchImportSkills = vi.fn();
    mockSkillListOptions.mockReturnValue({
      queryKey: ["workspaces", "ws-1", "skills"],
      queryFn: () =>
        Promise.resolve([
          {
            id: "existing-skill",
            workspace_id: "ws-1",
            name: "Gitee Helper",
            description: "Existing helper",
            config: {},
            created_by: "user-1",
            created_at: "2026-05-29T00:00:00Z",
            updated_at: "2026-05-29T00:00:00Z",
          },
        ]),
    });

    setApiInstance({
      discoverImportSkills,
      importSkill,
      batchImportSkills,
    } as unknown as ApiClient);

    renderDialog();

    await user.click(screen.getByRole("button", { name: /Import from URL/i }));
    await user.type(screen.getByLabelText("Skill URL"), "https://gitee.com/acme/helper");
    await user.click(screen.getByRole("button", { name: /^Import$/i }));

    expect(await screen.findByText("Skill name conflicts")).toBeInTheDocument();
    expect(batchImportSkills).not.toHaveBeenCalled();

    const conflictCheckbox = screen
      .getAllByText("Gitee Helper")
      .at(-1)!
      .closest("label")!
      .querySelector("[data-slot='checkbox']")!;
    fireEvent.click(conflictCheckbox);

    await user.click(screen.getByRole("button", { name: /Continue import/i }));

    await waitFor(() => {
      expect(importSkill).toHaveBeenCalledWith({
        url: "https://gitee.com/acme/helper/tree/master/helper",
        overwrite: true,
      });
    });
    expect(batchImportSkills).not.toHaveBeenCalled();
  });
});
