import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { setCurrentWorkspace } from "@multica/core/platform";
import { skillDetailOptions } from "@multica/core/workspace/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSkills from "../../locales/en/skills.json";

const TEST_RESOURCES = {
  en: { common: enCommon, skills: enSkills },
};

const mockImportSkillArchive = vi.hoisted(() => vi.fn());
const mockPrepareFromPicker = vi.hoisted(() => vi.fn());
const mockListSkills = vi.hoisted(() => vi.fn());
const mockWorkspaceId = vi.hoisted(() => ({ value: "ws-1" }));
const mockWrap = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const { ApiError } =
    await vi.importActual<typeof import("@multica/core/api")>(
      "@multica/core/api",
    );
  return {
    ApiError,
    api: {
      listSkills: (...args: unknown[]) => mockListSkills(...args),
      importSkillArchive: (...args: unknown[]) =>
        mockImportSkillArchive(...args),
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => mockWorkspaceId.value,
}));

vi.mock("@multica/core/skills", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/skills")>(
    "@multica/core/skills",
  );
  return {
    ...actual,
    discoverSkillArchivesFromPickerFiles: (...args: unknown[]) =>
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
  reconciliationName: "review-helper",
  preview: {
    displayName: "review-helper",
    skillName: "review-helper",
    description: "Reviews code changes",
    fileCount: 2,
    source: "folder" as const,
  },
};

function discovery(prepared = PREPARED_OK) {
  return {
    ok: true as const,
    items: [
      {
        id: prepared.preview.displayName,
        preview: prepared.preview,
        prepare: async () => prepared,
      },
    ],
  };
}

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
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nProvider>
  );
  return {
    onCreated,
    onClose,
    queryClient,
    ...render(<CreateSkillDialog onClose={onClose} onCreated={onCreated} />, {
      wrapper,
    }),
  };
}

describe("CreateSkillDialog local import", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockWorkspaceId.value = "ws-1";
    setCurrentWorkspace("workspace-a", "ws-1");
    mockPrepareFromPicker.mockResolvedValue(discovery());
    mockListSkills.mockResolvedValue([]);
    mockWrap.mockReturnValue({
      ...PREPARED_OK,
      reconciliationName: null,
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

  afterEach(() => {
    setCurrentWorkspace(null, null);
    vi.unstubAllGlobals();
  });

  it("waits for an explicit source choice before opening either picker", () => {
    renderDialog();
    const click = vi.fn();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement | null;
    expect(input).not.toBeNull();
    if (!input) return;
    input.click = click;
    const archiveClick = vi.fn();
    (document.querySelector("input[accept]") as HTMLInputElement).click =
      archiveClick;
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));
    expect(click).not.toHaveBeenCalled();
    expect(archiveClick).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /Choose folder/i }));
    expect(click).toHaveBeenCalledTimes(1);
    expect(archiveClick).not.toHaveBeenCalled();
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
      target: {
        files: [new File(["---\nname: review-helper\n---\n"], "SKILL.md")],
      },
    });

    expect(
      (await screen.findAllByText("review-helper")).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /^Import$/i })).toBeEnabled();
  });

  it("does not import when the folder has no SKILL.md", async () => {
    mockPrepareFromPicker.mockResolvedValue({
      ok: false,
      error: "missing_skill_md",
    });
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
      return discovery();
    });
    renderDialog();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    const file = new File(["---\nname: review-helper\n---\n"], "SKILL.md");
    installLiveFileList(input, [file]);
    fireEvent.change(input);

    expect(
      (await screen.findAllByText("review-helper")).length,
    ).toBeGreaterThan(0);
    expect(mockPrepareFromPicker).toHaveBeenCalled();
    const passed = mockPrepareFromPicker.mock.calls[0]![0] as File[];
    expect(Array.from(passed)).toHaveLength(1);
    installLiveFileList(input, [file]);
    fireEvent.change(input);
    await waitFor(() => expect(mockPrepareFromPicker).toHaveBeenCalledTimes(2));
    expect(input.value).toBe("");
  });

  it("imports a packed folder and closes", async () => {
    const { onCreated, onClose } = renderDialog();

    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    const file = new File(["---\nname: review-helper\n---\n"], "SKILL.md");
    fireEvent.change(input, { target: { files: [file] } });

    expect(
      (await screen.findAllByText("review-helper")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /^Import$/i }));

    await waitFor(() => {
      expect(mockImportSkillArchive).toHaveBeenCalledWith(
        ARCHIVE_FILE,
        "fail",
        { workspaceSlug: "workspace-a" },
      );
    });
    await waitFor(() => {
      expect(onCreated).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
    });
  });

  it("uploads a chosen .skill archive without re-packing", async () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));

    const archiveInput = document.querySelector(
      'input[type="file"][accept]',
    ) as HTMLInputElement;
    fireEvent.change(archiveInput, { target: { files: [ARCHIVE_FILE] } });

    expect(
      (await screen.findAllByText("review-helper")).length,
    ).toBeGreaterThan(0);
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
    const first = deferred<ReturnType<typeof discovery>>();
    const second = deferred<ReturnType<typeof discovery>>();
    mockPrepareFromPicker
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);

    renderDialog();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    fireEvent.change(input, {
      target: { files: [new File(["b"], "SKILL.md")] },
    });

    const preparedB = {
      ...PREPARED_OK,
      preview: {
        ...PREPARED_OK.preview,
        skillName: "skill-b",
        displayName: "skill-b",
      },
    };
    const preparedA = {
      ...PREPARED_OK,
      preview: {
        ...PREPARED_OK.preview,
        skillName: "skill-a",
        displayName: "skill-a",
      },
    };
    second.resolve(discovery(preparedB));
    expect((await screen.findAllByText("skill-b")).length).toBeGreaterThan(0);
    first.resolve(discovery(preparedA));
    await waitFor(() => {
      expect(screen.queryByText("skill-a")).not.toBeInTheDocument();
    });
    expect(screen.getAllByText("skill-b").length).toBeGreaterThan(0);
  });

  it("clears a previous import error when a new folder is picked", async () => {
    mockImportSkillArchive.mockRejectedValueOnce(
      new (await import("@multica/core/api")).ApiError(
        "a skill with this name already exists",
        409,
        "Conflict",
      ),
    );
    renderDialog();
    const input = document.querySelector(
      'input[type="file"][multiple]',
    ) as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    expect(
      (await screen.findAllByText("review-helper")).length,
    ).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: /^Import$/i }));
    expect(
      await screen.findByText(/a skill with this name already exists/i),
    ).toBeInTheDocument();

    mockPrepareFromPicker.mockResolvedValue(
      discovery({
        ...PREPARED_OK,
        preview: {
          ...PREPARED_OK.preview,
          skillName: "other-skill",
          displayName: "other-skill",
        },
      }),
    );
    fireEvent.change(input, {
      target: { files: [new File(["b"], "SKILL.md")] },
    });
    expect((await screen.findAllByText("other-skill")).length).toBeGreaterThan(
      0,
    );
    expect(
      screen.queryByText(/a skill with this name already exists/i),
    ).not.toBeInTheDocument();
  });
  function pickBatch(
    names = ["alpha", "beta", "gamma"],
    prepare?: (name: string) => Promise<typeof PREPARED_OK>,
  ) {
    mockPrepareFromPicker.mockResolvedValue({
      ok: true,
      items: names.map((name) => ({
        id: `skills/${name}/`,
        preview: { ...PREPARED_OK.preview, skillName: name },
        prepare: () =>
          prepare
            ? prepare(name)
            : Promise.resolve({
                ...PREPARED_OK,
                file: new File([name], `${name}.skill`),
                reconciliationName: name,
              }),
      })),
    });
    const rendered = renderDialog();
    fireEvent.change(document.querySelector("input[multiple]")!, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    return rendered;
  }

  it("imports only selected items, continues after conflicts and retries only failures", async () => {
    const { onClose, onCreated } = pickBatch();
    await screen.findByText("alpha");
    fireEvent.click(screen.getByRole("checkbox", { name: "gamma" }));
    mockImportSkillArchive.mockRejectedValueOnce(
      new (await import("@multica/core/api")).ApiError(
        "name exists",
        409,
        "Conflict",
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
    await screen.findByText("Imported: 1 · Failed or uncertain: 1");
    expect(
      mockImportSkillArchive.mock.calls.map((call) => call[0].name),
    ).toEqual(["alpha.skill", "beta.skill"]);
    expect(onClose).not.toHaveBeenCalled();
    expect(onCreated).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Import 1" }));
    await screen.findByText("Imported: 2 · Failed or uncertain: 0");
    expect(
      mockImportSkillArchive.mock.calls.map((call) => call[0].name),
    ).toEqual(["alpha.skill", "beta.skill", "alpha.skill"]);
    fireEvent.click(screen.getByRole("button", { name: "Done" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("locks dismissal and reselection during upload and stops only subsequent items", async () => {
    let resolve!: (value: { id: string; workspace_id: string }) => void;
    mockImportSkillArchive.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { onClose } = pickBatch();
    await screen.findByText("alpha");
    fireEvent.click(screen.getByRole("button", { name: "Import 3" }));
    await waitFor(() =>
      expect(mockImportSkillArchive).toHaveBeenCalledTimes(1),
    );
    expect(
      screen.getByRole("button", { name: /Choose folder/i }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: /Close/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /Back/i })).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Stop after current" }));
    resolve({ id: "alpha-id", workspace_id: "ws-1" });
    await screen.findByText("Imported: 1 · Failed or uncertain: 0");
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Import 2" })).toBeEnabled();
  });

  it("requires a successful refresh before explicitly retrying uncertain results", async () => {
    const { onClose } = pickBatch(["alpha", "beta"]);
    await screen.findByText("alpha");
    mockImportSkillArchive.mockRejectedValueOnce(
      new TypeError("network error"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
    await screen.findByText("Imported: 1 · Failed or uncertain: 1");
    expect(screen.getByRole("button", { name: "Import 0" })).toBeDisabled();
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(2);
    mockListSkills.mockRejectedValueOnce(new TypeError("offline"));
    fireEvent.click(screen.getByRole("button", { name: "Check results" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Check results" }),
      ).toBeEnabled(),
    );
    expect(screen.getByRole("button", { name: "Import 0" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Check results" }));
    await screen.findByText(
      "Not found in the refreshed list. You can retry this item.",
    );
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("button", { name: "Import 1" }));
    await screen.findByText("Imported: 2 · Failed or uncertain: 0");
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(3);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("leaves invalid items disabled while valid items remain importable", async () => {
    mockPrepareFromPicker.mockResolvedValue({
      ok: true,
      items: [
        ...discovery().items,
        {
          ...discovery().items[0],
          id: "oversized/",
          preview: { ...PREPARED_OK.preview, skillName: "oversized" },
          error: "too_large",
        },
      ],
    });
    renderDialog();
    fireEvent.change(document.querySelector("input[multiple]")!, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    await screen.findByText("This skill is too large to import.");
    expect(screen.getByRole("checkbox", { name: "oversized" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "oversized" }));
    expect(screen.getByRole("checkbox", { name: "oversized" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.getByRole("button", { name: "Import 1" })).toBeEnabled();
  });

  it("ignores a slow folder preview after an archive is chosen and handles cancellation", async () => {
    let resolve!: (value: ReturnType<typeof discovery>) => void;
    mockPrepareFromPicker.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: /Import from local/i }));
    const folder = document.querySelector("input[multiple]")!;
    fireEvent.change(folder, { target: { files: [] } });
    expect(screen.getByRole("button", { name: /^Import$/ })).toBeDisabled();
    fireEvent.change(folder, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    fireEvent.change(document.querySelector("input[accept]")!, {
      target: { files: [ARCHIVE_FILE] },
    });
    await screen.findByText("review-helper");
    resolve(
      discovery({
        ...PREPARED_OK,
        preview: { ...PREPARED_OK.preview, skillName: "stale" },
      }),
    );
    await waitFor(() =>
      expect(screen.queryByText("stale")).not.toBeInTheDocument(),
    );
    fireEvent.change(folder, { target: { files: [] } });
    expect(screen.getByRole("button", { name: /^Import$/ })).toBeEnabled();
  });
  it("keeps uncertain names locked when a refreshed list contains the same name", async () => {
    pickBatch(["alpha", "beta"]);
    await screen.findByText("alpha");
    mockImportSkillArchive.mockRejectedValueOnce(
      new (await import("@multica/core/api")).ApiError(
        "server error",
        500,
        "Internal Server Error",
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
    await screen.findByText("Imported: 1 · Failed or uncertain: 1");
    mockListSkills.mockResolvedValueOnce([
      { name: "alpha", workspace_id: "ws-1" },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Check results" }));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Check results" }),
      ).toBeEnabled(),
    );
    expect(screen.getByRole("button", { name: "Import 0" })).toBeDisabled();
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(2);
  });
  it("must stop queued writes after unmount and workspace change", async () => {
    const { ApiClient } =
      await vi.importActual<typeof import("@multica/core/api")>(
        "@multica/core/api",
      );
    const { setCurrentWorkspace } = await import("@multica/core/platform");
    const client = new ApiClient("https://api.example.test");
    let resolveFirst!: (value: Response) => void;
    const first = new Promise<Response>((resolve) => {
      resolveFirst = resolve;
    });
    const reply = (name: string) =>
      new Response(
        JSON.stringify({
          status: "created",
          skill: { id: name, workspace_id: "ws-1", name },
        }),
        { status: 201, headers: { "Content-Type": "application/json" } },
      );
    const fetchMock = vi
      .fn()
      .mockReturnValueOnce(first)
      .mockImplementation(async () => reply("beta"));
    vi.stubGlobal("fetch", fetchMock);
    setCurrentWorkspace("workspace-a", "ws-1");
    try {
      mockImportSkillArchive.mockImplementation(
        (...args: Parameters<typeof client.importSkillArchive>) =>
          client.importSkillArchive(...args),
      );
      const { unmount, queryClient, onClose, onCreated } = pickBatch([
        "alpha",
        "beta",
      ]);
      await screen.findByText("alpha");
      fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
      unmount();
      setCurrentWorkspace("workspace-b", "ws-2");
      await act(async () => {
        resolveFirst(reply("alpha"));
        await first;
      });
      expect(
        fetchMock.mock.calls.map((call) => call[1].headers["X-Workspace-Slug"]),
      ).toEqual(["workspace-a"]);
      expect(
        queryClient.getQueryData(skillDetailOptions("ws-1", "alpha").queryKey),
      ).toMatchObject({ id: "alpha", workspace_id: "ws-1" });
      expect(
        queryClient.getQueryData(skillDetailOptions("ws-2", "alpha").queryKey),
      ).toBeUndefined();
      expect(onClose).not.toHaveBeenCalled();
      expect(onCreated).not.toHaveBeenCalled();
    } finally {
      setCurrentWorkspace(null, null);
      vi.unstubAllGlobals();
    }
  });

  it.each(["alpha", "download", "empty"])(
    "keeps archive identity unknown when the verified list contains %s",
    async (listedName) => {
      const actual = await vi.importActual<
        typeof import("@multica/core/skills")
      >("@multica/core/skills");
      mockWrap.mockImplementation(actual.wrapExistingSkillArchive);
      const packed = actual.packStoreZip([
        {
          path: "alpha/SKILL.md",
          data: new TextEncoder().encode("---\nname: alpha\n---\n# Alpha\n"),
        },
      ]);
      const archive = new File(
        [new Uint8Array(packed).buffer],
        "download.zip",
        { type: "application/zip" },
      );
      renderDialog();
      fireEvent.change(document.querySelector("input[accept]")!, {
        target: { files: [archive] },
      });
      await screen.findByText("download");
      mockImportSkillArchive.mockRejectedValueOnce(
        new TypeError("response lost after server committed alpha"),
      );
      fireEvent.click(screen.getByRole("button", { name: /^Import$/ }));
      await screen.findByRole("button", { name: "Check results" });
      mockListSkills.mockResolvedValueOnce(
        listedName === "empty"
          ? []
          : [{ id: listedName, workspace_id: "ws-1", name: listedName }],
      );
      fireEvent.click(screen.getByRole("button", { name: "Check results" }));
      await waitFor(() => expect(mockListSkills).toHaveBeenCalledTimes(1));
      await act(async () => {
        await Promise.resolve();
      });
      expect(screen.getByRole("button", { name: /^Import$/ })).toBeDisabled();
      expect(screen.getByText(/skill name is unknown/i)).toBeInTheDocument();
      expect(mockImportSkillArchive).toHaveBeenCalledTimes(1);
    },
  );

  it.each([
    { body: { skills: [] }, verified: false },
    { body: [], verified: true },
  ])(
    "only unlocks directory imports with a verified list: $verified",
    async ({ body, verified }) => {
      const { ApiClient } =
        await vi.importActual<typeof import("@multica/core/api")>(
          "@multica/core/api",
        );
      const client = new ApiClient("https://api.example.test");
      const { onClose } = pickBatch(["alpha", "beta"]);
      await screen.findByText("alpha");
      mockImportSkillArchive.mockRejectedValueOnce(
        new TypeError("response lost"),
      );
      fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
      await screen.findByText("Imported: 1 · Failed or uncertain: 1");
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      mockListSkills.mockImplementation(
        (...args: Parameters<typeof client.listSkills>) =>
          client.listSkills(...args),
      );
      try {
        fireEvent.click(screen.getByRole("button", { name: "Check results" }));
        await waitFor(() => expect(mockListSkills).toHaveBeenCalledTimes(1));
        await act(async () => {
          await Promise.resolve();
        });
        expect(mockListSkills).toHaveBeenCalledWith({
          requireValidResponse: true,
          workspaceSlug: "workspace-a",
        });
        if (verified) {
          expect(
            screen.getByText(
              "Not found in the refreshed list. You can retry this item.",
            ),
          ).toBeInTheDocument();
          expect(
            screen.getByRole("button", { name: "Import 1" }),
          ).toBeEnabled();
        } else {
          expect(
            screen.queryByText(
              "Not found in the refreshed list. You can retry this item.",
            ),
          ).not.toBeInTheDocument();
          expect(
            screen.getByRole("button", { name: "Import 0" }),
          ).toBeDisabled();
          expect(
            screen.getByText(/Could not verify the skill list/i),
          ).toBeInTheDocument();
        }
        expect(onClose).not.toHaveBeenCalled();
      } finally {
        vi.unstubAllGlobals();
      }
    },
  );
  it("does not upload when unmounted during asynchronous archive preparation", async () => {
    const { ApiClient } =
      await vi.importActual<typeof import("@multica/core/api")>(
        "@multica/core/api",
      );
    const client = new ApiClient("https://api.example.test");
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    mockImportSkillArchive.mockImplementation(
      (...args: Parameters<typeof client.importSkillArchive>) =>
        client.importSkillArchive(...args),
    );
    let resolve!: (value: typeof PREPARED_OK) => void;
    const prepare = vi.fn(
      () =>
        new Promise<typeof PREPARED_OK>((r) => {
          resolve = r;
        }),
    );
    const { unmount, queryClient } = pickBatch(["alpha", "beta"], prepare);
    await screen.findByText("alpha");
    fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
    expect(prepare).toHaveBeenCalledTimes(1);
    unmount();
    setCurrentWorkspace("workspace-b", "ws-2");
    await act(async () => {
      resolve(PREPARED_OK);
    });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(prepare).toHaveBeenCalledTimes(1);
    expect(
      queryClient.getQueryData(skillDetailOptions("ws-1", "alpha").queryKey),
    ).toBeUndefined();
  });

  it.each(["preparing", "uploading"])(
    "stops an A queue when workspace changes without unmounting during %s",
    async (phase) => {
      const { ApiClient } =
        await vi.importActual<typeof import("@multica/core/api")>(
          "@multica/core/api",
        );
      const client = new ApiClient("https://api.example.test");
      let resolveUpload!: (value: Response) => void;
      const fetchMock = vi.fn(
        (_url: string, _init?: RequestInit) =>
          new Promise<Response>((r) => {
            resolveUpload = r;
          }),
      );
      vi.stubGlobal("fetch", fetchMock);
      mockImportSkillArchive.mockImplementation(
        (...args: Parameters<typeof client.importSkillArchive>) =>
          client.importSkillArchive(...args),
      );
      let resolvePrepare!: (value: typeof PREPARED_OK) => void;
      const prepare =
        phase === "preparing"
          ? vi.fn(
              () =>
                new Promise<typeof PREPARED_OK>((r) => {
                  resolvePrepare = r;
                }),
            )
          : undefined;
      const { queryClient, rerender, onClose, onCreated } = pickBatch(
        ["alpha", "beta"],
        prepare,
      );
      const invalidate = vi.spyOn(queryClient, "invalidateQueries");
      const aList = [{ id: "existing-a" }];
      const bList = [{ id: "existing-b" }];
      queryClient.setQueryData(["workspaces", "ws-1", "skills"], aList);
      queryClient.setQueryData(["workspaces", "ws-2", "skills"], bList);
      await screen.findByText("alpha");
      fireEvent.click(screen.getByRole("button", { name: "Import 2" }));
      if (phase === "uploading")
        await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
      mockWorkspaceId.value = "ws-2";
      await act(async () => {
        setCurrentWorkspace("workspace-b", "ws-2");
        rerender(<CreateSkillDialog onClose={onClose} onCreated={onCreated} />);
      });
      if (phase === "preparing") {
        await act(async () => {
          resolvePrepare(PREPARED_OK);
        });
        expect(fetchMock).not.toHaveBeenCalled();
        expect(prepare).toHaveBeenCalledTimes(1);
      } else {
        await act(async () => {
          resolveUpload(
            new Response(
              JSON.stringify({
                status: "created",
                skill: { id: "alpha", workspace_id: "ws-1", name: "alpha" },
              }),
              { status: 201 },
            ),
          );
        });
        expect(
          fetchMock.mock.calls.map((call) => (call[1] as RequestInit).headers),
        ).toEqual([
          expect.objectContaining({ "X-Workspace-Slug": "workspace-a" }),
        ]);
        expect(
          queryClient.getQueryData(
            skillDetailOptions("ws-1", "alpha").queryKey,
          ),
        ).toMatchObject({ id: "alpha", workspace_id: "ws-1" });
        expect(
          queryClient.getQueryData(
            skillDetailOptions("ws-2", "alpha").queryKey,
          ),
        ).toBeUndefined();
        expect(invalidate).toHaveBeenCalledWith({
          queryKey: ["workspaces", "ws-1", "skills"],
          refetchType: "none",
        });
        expect(
          screen.getByText("Imported: 1 · Failed or uncertain: 0"),
        ).toBeInTheDocument();
      }
      expect(
        queryClient.getQueryData(["workspaces", "ws-1", "skills"]),
      ).toEqual(aList);
      expect(
        queryClient.getQueryData(["workspaces", "ws-2", "skills"]),
      ).toEqual(bList);
      expect(screen.getByText(/workspace changed/i)).toBeInTheDocument();
      expect(onClose).not.toHaveBeenCalled();
      expect(onCreated).not.toHaveBeenCalled();
    },
  );
  it("retains selection ownership when workspace changes before the local panel commits", async () => {
    let resolve!: (value: ReturnType<typeof discovery>) => void;
    mockPrepareFromPicker.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { rerender, onClose, onCreated } = renderDialog();
    await act(async () => {
      fireEvent.change(document.querySelector("input[multiple]")!, {
        target: { files: [new File(["a"], "SKILL.md")] },
      });
      mockWorkspaceId.value = "ws-2";
      setCurrentWorkspace("workspace-b", "ws-2");
      rerender(<CreateSkillDialog onClose={onClose} onCreated={onCreated} />);
    });
    await act(async () => {
      resolve(discovery());
    });
    expect(screen.getByText(/workspace changed/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Import$/ })).toBeDisabled();
    expect(mockImportSkillArchive).not.toHaveBeenCalled();
  });
  it("keeps a BOM directory uncertain when its server fallback name exists", async () => {
    const actual = await vi.importActual<typeof import("@multica/core/skills")>(
      "@multica/core/skills",
    );
    const packed = actual.prepareSkillArchiveFromEntries([
      {
        relativePath: "download/SKILL.md",
        data: new TextEncoder().encode("\ufeff---\nname: alpha\n---\n# Alpha"),
      },
    ]);
    if (!packed.ok) throw new Error("packing failed");
    mockPrepareFromPicker.mockResolvedValueOnce({
      ok: true,
      items: [
        {
          id: "download",
          preview: packed.preview,
          prepare: async () => packed,
        },
      ],
    });
    renderDialog();
    fireEvent.change(document.querySelector("input[multiple]")!, {
      target: { files: [new File(["bom"], "SKILL.md")] },
    });
    await screen.findByText("alpha");
    mockImportSkillArchive.mockRejectedValueOnce(
      new TypeError("response lost after server created download"),
    );
    fireEvent.click(screen.getByRole("button", { name: /^Import$/ }));
    await screen.findByRole("button", { name: "Check results" });
    mockListSkills.mockResolvedValueOnce([
      { id: "download-id", workspace_id: "ws-1", name: "download" },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Check results" }));
    await waitFor(() => expect(mockListSkills).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Check results" }),
      ).toBeEnabled(),
    );
    expect(
      screen.queryByText(
        "Not found in the refreshed list. You can retry this item.",
      ),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Import$/ })).toBeDisabled();
    expect(mockImportSkillArchive).toHaveBeenCalledTimes(1);
    expect(mockListSkills).toHaveBeenCalledWith({
      requireValidResponse: true,
      workspaceSlug: "workspace-a",
    });
    expect(
      screen.getByText("Imported: 0 · Failed or uncertain: 1"),
    ).toBeInTheDocument();
  });

  it("keeps a late single success in A without closing or navigating", async () => {
    let resolve!: (value: {
      id: string;
      workspace_id: string;
      name: string;
    }) => void;
    mockImportSkillArchive.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    const { onClose, onCreated, queryClient, unmount } = renderDialog();
    fireEvent.change(document.querySelector("input[multiple]")!, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    await screen.findAllByText("review-helper");
    fireEvent.click(screen.getByRole("button", { name: /^Import$/ }));
    await waitFor(() =>
      expect(mockImportSkillArchive).toHaveBeenCalledTimes(1),
    );
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    unmount();
    setCurrentWorkspace("workspace-b", "ws-2");
    await act(async () => {
      resolve({ id: "late-alpha", workspace_id: "ws-1", name: "alpha" });
    });
    expect(
      queryClient.getQueryData(
        skillDetailOptions("ws-1", "late-alpha").queryKey,
      ),
    ).toMatchObject({ workspace_id: "ws-1" });
    expect(
      queryClient.getQueryData(
        skillDetailOptions("ws-2", "late-alpha").queryKey,
      ),
    ).toBeUndefined();
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["workspaces", "ws-1", "skills"],
      refetchType: "none",
    });
    expect(onClose).not.toHaveBeenCalled();
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("invalidates A without refetch after a late single upload failure", async () => {
    let reject!: (value: Error) => void;
    mockImportSkillArchive.mockReturnValueOnce(
      new Promise((_, r) => {
        reject = r;
      }),
    );
    const { queryClient, unmount } = renderDialog();
    fireEvent.change(document.querySelector("input[multiple]")!, {
      target: { files: [new File(["a"], "SKILL.md")] },
    });
    await screen.findAllByText("review-helper");
    fireEvent.click(screen.getByRole("button", { name: /^Import$/ }));
    await waitFor(() =>
      expect(mockImportSkillArchive).toHaveBeenCalledTimes(1),
    );
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    unmount();
    setCurrentWorkspace("workspace-b", "ws-2");
    await act(async () => {
      reject(new TypeError("network error"));
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["workspaces", "ws-1", "skills"],
      refetchType: "none",
    });
    expect(invalidate).not.toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["workspaces", "ws-2", "skills"] }),
    );
  });
});
