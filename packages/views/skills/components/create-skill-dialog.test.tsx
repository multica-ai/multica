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

vi.mock("../../platform", () => ({
  openExternal: mockOpenExternal,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
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
});
