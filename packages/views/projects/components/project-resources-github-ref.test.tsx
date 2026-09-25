// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, fireEvent, waitFor, within } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const updateMock = vi.fn().mockResolvedValue({});
const createMock = vi.fn().mockResolvedValue({});

const PINNED = {
  id: "res-1",
  project_id: "p1",
  workspace_id: "workspace-1",
  resource_type: "github_repo",
  resource_ref: {
    url: "https://github.com/multica-ai/multica",
    ref: "release/2026-09",
  },
  // A custom name, which is what used to hide the ref: the row rendered
  // `label || owner/repo @ ref`, so naming a repo erased the one signal that
  // its tasks do not start from the default branch.
  label: "Release line",
  position: 0,
  created_at: "2026-09-19T00:00:00Z",
  created_by: "u1",
};

// The common case: no ref, so tasks use the repository's default branch.
const PLAIN = {
  id: "res-2",
  project_id: "p1",
  workspace_id: "workspace-1",
  resource_type: "github_repo",
  resource_ref: { url: "https://github.com/multica-ai/docs" },
  label: null,
  position: 1,
  created_at: "2026-09-19T00:00:00Z",
  created_by: "u1",
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options?.queryKey?.[0];
    if (key === "project-resources") return { data: [PINNED, PLAIN] };
    return { data: [] };
  },
  queryOptions: (options: unknown) => options,
}));

vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: () => ({ queryKey: ["project-resources"], queryFn: vi.fn() }),
  useCreateProjectResource: () => ({ mutateAsync: createMock, isPending: false }),
  useUpdateProjectResource: () => ({ mutateAsync: updateMock }),
  useDeleteProjectResource: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { localWorktreeSupported: boolean }) => unknown) =>
    selector({ localWorktreeSupported: true }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: vi.fn() }),
  runtimeAdvertisesLocalWorktree: () => true,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", slug: "ws", repos: [] }),
}));
vi.mock("../../platform/local-directory", () => ({
  isDesktopShell: () => false,
  pickDirectory: vi.fn(),
  validateLocalDirectory: vi.fn(),
}));
vi.mock("../../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => ({ daemonId: null, deviceName: null, running: false }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ProjectResourcesSection } from "./project-resources-section";

describe("ProjectResourcesSection — github_repo checkout ref", () => {
  beforeEach(() => {
    updateMock.mockClear();
    createMock.mockClear();
  });

  it("shows the ref beside a repo that has a custom label", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);
    expect(screen.getByText("Release line")).toBeTruthy();
    expect(screen.getByText("release/2026-09")).toBeTruthy();
  });

  it("saves a new ref while preserving the rest of the stored ref", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getAllByTitle(/change the branch tasks work on/i)[0]!);
    const input = screen.getByLabelText(/starting branch/i);
    fireEvent.change(input, { target: { value: "release/2026-10" } });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1));
    const payload = updateMock.mock.calls[0]?.[0] as {
      resourceId: string;
      data: { resource_ref: Record<string, unknown> };
    };
    expect(payload.resourceId).toBe("res-1");
    // The URL must ride along: the server replaces the whole ref rather than
    // deep-merging, so an edit that sends only `ref` is a 400 at best and
    // drops the repository at worst.
    expect(payload.data.resource_ref).toEqual({
      url: "https://github.com/multica-ai/multica",
      ref: "release/2026-10",
    });
  });

  it("clearing the field drops the key so tasks fall back to the default branch", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getAllByTitle(/change the branch tasks work on/i)[0]!);
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1));
    const payload = updateMock.mock.calls[0]?.[0] as {
      data: { resource_ref: Record<string, unknown> };
    };
    expect(payload.data.resource_ref).toEqual({
      url: "https://github.com/multica-ai/multica",
      ref: undefined,
    });
  });

  it("refuses to save a ref git could never resolve", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getAllByTitle(/change the branch tasks work on/i)[0]!);
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "main..dev" },
    });

    const save = screen.getByRole("button", { name: /^save$/i }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(screen.getByText(/not a valid branch, tag, or commit/i)).toBeTruthy();
    fireEvent.click(save);
    expect(updateMock).not.toHaveBeenCalled();
  });

  it("attaches a repo with the ref typed alongside its URL", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: /add resource/i }));
    const urlInput = screen.getByLabelText(/attach a github repo/i);
    fireEvent.change(urlInput, {
      target: { value: "https://github.com/multica-ai/other" },
    });
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "main" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock.mock.calls[0]?.[0]).toEqual({
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/multica-ai/other", ref: "main" },
    });
  });

  it("splits a pasted /tree/ URL into the repo and the branch it points at", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: /add resource/i }));
    // What someone copies out of the address bar when they want a branch.
    // Stored whole, this is a clone URL that does not exist.
    fireEvent.change(screen.getByLabelText(/attach a github repo/i), {
      target: { value: "https://github.com/multica-ai/other/tree/release/2026-09" },
    });

    const urlInput = screen.getByLabelText(/attach a github repo/i) as HTMLInputElement;
    const refInput = screen.getByLabelText(/starting branch/i) as HTMLInputElement;
    expect(urlInput.value).toBe("https://github.com/multica-ai/other");
    expect(refInput.value).toBe("release/2026-09");

    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));
    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock.mock.calls[0]?.[0]).toEqual({
      resource_type: "github_repo",
      resource_ref: {
        url: "https://github.com/multica-ai/other",
        ref: "release/2026-09",
      },
    });
  });

  // Regression: the split was gated on the branch field being empty, so a
  // SECOND pasted browse URL went in whole as the clone URL.
  it("normalises a pasted browse URL even when a branch is already filled in", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: /add resource/i }));
    const urlInput = () => screen.getByLabelText(/attach a github repo/i) as HTMLInputElement;
    const refInput = () => screen.getByLabelText(/starting branch/i) as HTMLInputElement;

    fireEvent.change(urlInput(), {
      target: { value: "https://github.com/multica-ai/one/tree/release/2026-09" },
    });
    expect(urlInput().value).toBe("https://github.com/multica-ai/one");
    expect(refInput().value).toBe("release/2026-09");

    fireEvent.change(urlInput(), {
      target: { value: "https://github.com/multica-ai/two/tree/main" },
    });
    expect(urlInput().value).toBe("https://github.com/multica-ai/two");
    expect(refInput().value).toBe("main");

    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));
    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock.mock.calls[0]?.[0]).toEqual({
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/multica-ai/two", ref: "main" },
    });
  });

  it("omits the ref key entirely when the field is left empty", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: /add resource/i }));
    fireEvent.change(screen.getByLabelText(/attach a github repo/i), {
      target: { value: "https://github.com/multica-ai/other" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1));
    expect(createMock.mock.calls[0]?.[0]).toEqual({
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/multica-ai/other" },
    });
  });

  it("blocks attaching when the ref is invalid, without touching the URL field", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getByRole("button", { name: /add resource/i }));
    fireEvent.change(screen.getByLabelText(/attach a github repo/i), {
      target: { value: "https://github.com/multica-ai/other" },
    });
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "bad ref" },
    });

    const add = screen.getByRole("button", { name: /^add$/i }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    fireEvent.click(add);
    expect(createMock).not.toHaveBeenCalled();
  });

  // Clearing a branch used to make the line vanish, which reads the same as
  // the setting never having existed. An unpinned repo says so instead.
  it("says 'Default branch' when nothing is pinned, rather than showing nothing", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    // `.group` is the row root; the link's immediate parent is only its top line.
    const pinnedRow = screen.getByText("Release line").closest(".group") as HTMLElement;
    expect(within(pinnedRow).queryByText("release/2026-09")).toBeTruthy();
    expect(within(pinnedRow).queryByText(/default branch/i)).toBeNull();

    const plainRow = screen.getByText("multica-ai/docs").closest(".group") as HTMLElement;
    expect(within(plainRow).queryByText(/default branch/i)).toBeTruthy();
    expect(within(plainRow).queryByText("release/2026-09")).toBeNull();
  });

  // The field asks for a branch because a pinned start is also the PR target,
  // and a commit has nothing to merge back into. Only a full-length object id
  // is refused — `v1.2.3` is a legal branch name, so the rest cannot be told
  // apart without asking the remote, which the product does not do.
  it("declines a pasted commit id and points at the per-task escape hatch", () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getAllByTitle(/change the branch tasks work on/i)[0]!);
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c" },
    });

    const save = screen.getByRole("button", { name: /^save$/i }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    expect(screen.getByText(/commit, not a branch/i)).toBeTruthy();
    expect(screen.getByText(/--ref/)).toBeTruthy();
    expect(updateMock).not.toHaveBeenCalled();
  });

  it("still accepts a tag-shaped name — it could be a branch, and only the remote knows", async () => {
    renderWithI18n(<ProjectResourcesSection projectId="p1" />);

    fireEvent.click(screen.getAllByTitle(/change the branch tasks work on/i)[0]!);
    fireEvent.change(screen.getByLabelText(/starting branch/i), {
      target: { value: "v1.4.0" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(updateMock).toHaveBeenCalledTimes(1));
  });
});
