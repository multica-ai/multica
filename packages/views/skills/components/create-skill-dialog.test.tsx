import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";

const TEST_RESOURCES = {
  en: { common: enCommon, skills: enSkills },
};

const mockImportSkillArchive = vi.hoisted(() => vi.fn());
const mockPrepareFromPicker = vi.hoisted(() => vi.fn());
const mockWrap = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    importSkillArchive: (...args: unknown[]) => mockImportSkillArchive(...args),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/skills", async () => {
  const actual = await vi.importActual<
    typeof import("@multica/core/skills")
  >("@multica/core/skills");
  return {
    ...actual,
    prepareSkillArchiveFromPickerFiles: (...args: unknown[]) =>
      mockPrepareFromPicker(...args),
    wrapExistingSkillArchive: (...args: unknown[]) => mockWrap(...args),
  };
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { CreateSkillDialog } from "./create-skill-dialog";

const ARCHIVE_FILE = new File(["pk"], "review-helper.skill", {
  type: "application/zip",
});

const PREPARED_OK = {
  ok: true as const,
  file: ARCHIVE_FILE,
  preview: {
    displayName: "review-helper",
    skillName: "review-helper",
    description: "Reviews code changes",
    fileCount: 2,
    source: "folder" as const,
  },
};

/**
 * Chromium's input.files is a live FileList: setting input.value = "" empties
 * it. jsdom's fireEvent `{ target: { files } }` bypasses that, so this helper
 * installs a live list to pin the snapshot-before-reset behaviour.
 */
function installLiveFileList(input: HTMLInputElement, files: File[]) {
  let current = files.slice();
  const list = () => {
    const live = Object.assign(current.slice(), {
      length: current.length,
      item: (i: number) => current[i] ?? null,
      [Symbol.iterator]: function* () {
        yield* current;
      },
    });
    return live as unknown as FileList;
  };
  Object.defineProperty(input, "files", {
    configurable: true,
    get: list,
  });
  Object.defineProperty(input, "value", {
    configurable: true,
    get: () => (current[0] ? `C:\\fakepath\\${current[0].name}` : ""),
    set: (next: string) => {
      if (next === "") current = [];
    },
  });
}

function renderDialog(onCreated = vi.fn(), onClose = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    onCreated,
    onClose,
    ...render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <CreateSkillDialog onClose={onClose} onCreated={onCreated} />
        </QueryClientProvider>
      </I18nProvider>,
    ),
  };
}

describe("CreateSkillDialog local import", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPrepareFromPicker.mockResolvedValue(PREPARED_OK);
    mockWrap.mockReturnValue({
      ...PREPARED_OK,
      preview: { ...PREPARED_OK.preview, source: "archive", fileCount: null },
    });
    mockImportSkillArchive.mockResolvedValue({
      id: "skill-1",
      workspace_id: "ws-1",
      name: "review-helper",
      description: "Reviews code changes",
      content: "# Review Helper",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    });
  });

  it("lists Import from local as the second method", () => {
    renderDialog();
    const cards = screen.getAllByRole("button");
    const titles = cards
      .map((el) => el.textContent ?? "")
      .filter((text) =>
        /Create manually|Import from local|Import from URL|Copy from runtime/.test(
          text,
        ),
      );
    expect(titles[0]).toContain("Create manually");
    expect(titles[1]).toContain("Import from local");
    expect(titles[2]).toContain("Import from URL");
    expect(titles[3]).toContain("Copy from runtime");
  });

  it("opens the folder picker when Import from local is clicked", () => {
    renderDialog();
    const click = vi.fn();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement | null;
    expect(input).not.toBeNull();
    if (!input) return;
    input.click = click;
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));
    expect(click).toHaveBeenCalled();
  });

  // The local panel keeps its footer at Cancel + Import: a footer that also
  // carried both source pickers overflowed the dialog and clipped Cancel
  // (MUL-6794). The pickers live in the panel body instead.
  it("offers both source pickers in the panel body", () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));

    const archiveInput = document.querySelector(
      'input[type="file"][accept]',
    ) as HTMLInputElement;
    const click = vi.fn();
    archiveInput.click = click;
    fireEvent.click(
      screen.getByRole("button", { name: /Choose \.skill \/ \.zip/i }),
    );
    expect(click).toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: /Choose folder/i }),
    ).toBeInTheDocument();
  });

  it("keeps Import disabled until a folder is prepared", async () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));
    expect(screen.getByRole("button", { name: /^Import$/i })).toBeDisabled();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [new File(["---\nname: review-helper\n---\n"], "SKILL.md")] },
    });

    expect((await screen.findAllByText("review-helper")).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /^Import$/i })).toBeEnabled();
  });

  it("does not import when the folder has no SKILL.md", async () => {
    mockPrepareFromPicker.mockResolvedValue({ ok: false, error: "missing_skill_md" });
    renderDialog();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    const file = new File(["# no"], "readme.md", { type: "text/markdown" });
    fireEvent.change(input, { target: { files: [file] } });

    expect(
      await screen.findByText(/This folder is not a skill/i),
    ).toBeInTheDocument();
    expect(mockImportSkillArchive).not.toHaveBeenCalled();
  });

  it("snapshots folder files before resetting the live FileList", async () => {
    mockPrepareFromPicker.mockImplementation(async (list: File[]) => {
      if (list.length === 0) {
        throw new Error("live FileList was emptied before snapshot");
      }
      return PREPARED_OK;
    });
    renderDialog();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    const file = new File(["---\nname: review-helper\n---\n"], "SKILL.md");
    installLiveFileList(input, [file]);
    fireEvent.change(input);

    expect((await screen.findAllByText("review-helper")).length).toBeGreaterThan(0);
    expect(mockPrepareFromPicker).toHaveBeenCalled();
    const passed = mockPrepareFromPicker.mock.calls[0]![0] as File[];
    expect(Array.from(passed)).toHaveLength(1);
  });

  it("imports a packed folder and closes", async () => {
    const { onCreated, onClose } = renderDialog();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    const file = new File(["---\nname: review-helper\n---\n"], "SKILL.md");
    fireEvent.change(input, { target: { files: [file] } });

    expect((await screen.findAllByText("review-helper")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /^Import$/i }));

    await waitFor(() => {
      expect(mockImportSkillArchive).toHaveBeenCalledWith(ARCHIVE_FILE, "fail");
    });
    await waitFor(() => {
      expect(onCreated).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
    });
  });

  it("uploads a chosen .skill archive without re-packing", async () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));

    // Stay on chooser until a file is picked; open the local panel via archive.
    const archiveInput = document.querySelector(
      'input[type="file"][accept]',
    ) as HTMLInputElement;
    fireEvent.change(archiveInput, { target: { files: [ARCHIVE_FILE] } });

    expect((await screen.findAllByText("review-helper")).length).toBeGreaterThan(0);
    expect(mockWrap).toHaveBeenCalledWith(ARCHIVE_FILE);
    fireEvent.click(screen.getByRole("button", { name: /^Import$/i }));
    await waitFor(() => {
      expect(mockImportSkillArchive).toHaveBeenCalled();
    });
    expect(mockPrepareFromPicker).not.toHaveBeenCalled();
  });

  it("ignores a slower earlier folder selection", async () => {
    type Deferred<T> = { promise: Promise<T>; resolve: (value: T) => void };
    const deferred = <T,>(): Deferred<T> => {
      let resolve!: (value: T) => void;
      const promise = new Promise<T>((r) => {
        resolve = r;
      });
      return { promise, resolve };
    };
    const first = deferred<typeof PREPARED_OK>();
    const second = deferred<typeof PREPARED_OK>();
    mockPrepareFromPicker
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    renderDialog();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["a"], "SKILL.md")] } });
    fireEvent.change(input, { target: { files: [new File(["b"], "SKILL.md")] } });

    const preparedB = {
      ...PREPARED_OK,
      preview: { ...PREPARED_OK.preview, skillName: "skill-b", displayName: "skill-b" },
    };
    const preparedA = {
      ...PREPARED_OK,
      preview: { ...PREPARED_OK.preview, skillName: "skill-a", displayName: "skill-a" },
    };
    second.resolve(preparedB);
    expect((await screen.findAllByText("skill-b")).length).toBeGreaterThan(0);
    first.resolve(preparedA);
    await waitFor(() => {
      expect(screen.queryByText("skill-a")).not.toBeInTheDocument();
    });
    expect(screen.getAllByText("skill-b").length).toBeGreaterThan(0);
  });

  it("clears a previous import error when a new folder is picked", async () => {
    mockImportSkillArchive.mockRejectedValueOnce(
      new Error("a skill with this name already exists"),
    );
    renderDialog();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["a"], "SKILL.md")] } });
    expect((await screen.findAllByText("review-helper")).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /^Import$/i }));
    expect(
      await screen.findByText(/a skill with this name already exists/i),
    ).toBeInTheDocument();

    mockPrepareFromPicker.mockResolvedValue({
      ...PREPARED_OK,
      preview: {
        ...PREPARED_OK.preview,
        skillName: "other-skill",
        displayName: "other-skill",
      },
    });
    fireEvent.change(input, { target: { files: [new File(["b"], "SKILL.md")] } });
    expect((await screen.findAllByText("other-skill")).length).toBeGreaterThan(0);
    expect(
      screen.queryByText(/a skill with this name already exists/i),
    ).not.toBeInTheDocument();
  });
});
