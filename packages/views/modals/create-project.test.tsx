import React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";

const longRepoUrl =
  "https://github.com/multica-ai/a-very-long-repository-name-that-needs-a-tooltip";
const apiRepoUrl = "https://github.com/multica-ai/api";
const webRepoUrl = "https://github.com/multica-ai/web";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
  // The modal now reads the runtime list to gate worktree mode, and
  // runtimeListOptions builds its descriptor with queryOptions.
  queryOptions: (options: unknown) => options,
}));

const createProjectMock = vi.hoisted(() => vi.fn().mockResolvedValue({ id: "p1" }));

vi.mock("@multica/core/projects/mutations", () => ({
  useCreateProject: () => ({ mutateAsync: createProjectMock }),
}));

vi.mock("@multica/core/projects", () => ({
  useProjectDraftStore: (selector: (state: unknown) => unknown) =>
    selector({
      draft: {
        title: "",
        description: "",
        status: "planned",
        priority: "medium",
        leadType: undefined,
        leadId: undefined,
        icon: undefined,
      },
      setDraft: vi.fn(),
      clearDraft: vi.fn(),
    }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    repos: [{ url: longRepoUrl }, { url: apiRepoUrl }, { url: webRepoUrl }],
  }),
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/test-workspace/projects/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: vi.fn() }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("../editor", () => {
  // Exposes the imperative handle the modal actually calls on submit
  // (getMarkdown). A plain textarea ref makes every submit throw, which is
  // invisible until a test clicks Create Project.
  const ContentEditor = React.forwardRef<
    { getMarkdown: () => string },
    { placeholder?: string }
  >(({ placeholder }, ref) => {
    const inner = React.useRef<HTMLTextAreaElement>(null);
    React.useImperativeHandle(ref, () => ({
      getMarkdown: () => inner.current?.value ?? "",
    }));
    return <textarea ref={inner} placeholder={placeholder} />;
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    ContentEditor,
    // Wires onSubmit to Enter the way the real editor does. Without it the
    // keyboard path into handleSubmit is untestable, and that path bypasses
    // the submit button's disabled state entirely.
    TitleEditor: ({
      placeholder,
      onChange,
      onSubmit,
    }: {
      placeholder?: string;
      onChange?: (value: string) => void;
      onSubmit?: () => void;
    }) => (
      <input
        placeholder={placeholder}
        onChange={(e) => onChange?.(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            onSubmit?.();
          }
        }}
      />
    ),
  };
});

vi.mock("../issues/components/priority-icon", () => ({
  PriorityIcon: () => <span data-testid="priority-icon" />,
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

// Stub the date pickers so this test doesn't pull the real Calendar (and its
// buttonVariants import) into the modal's module graph; the pickers have their
// own test. The stubs render the placeholder label so the pills are assertable.
vi.mock("../projects/components/project-start-date-picker", () => ({
  ProjectStartDatePicker: () => <button type="button">Start date</button>,
}));

vi.mock("../projects/components/project-due-date-picker", () => ({
  ProjectDueDatePicker: () => <button type="button">Due date</button>,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/common/emoji-picker", () => ({
  EmojiPicker: () => null,
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) =>
    values.filter(Boolean).join(" "),
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { CreateProjectModal } from "./create-project";

describe("CreateProjectModal", () => {
  it("exposes full repository URLs in the repository picker", () => {
    render(<CreateProjectModal onClose={vi.fn()} />);

    // The Tooltip is the single reveal mechanism. A native `title` carrying the
    // same URL would stack a browser tooltip on top of it (MUL-4836).
    expect(screen.getByRole("tooltip", { name: longRepoUrl })).toBeInTheDocument();
    expect(screen.queryByTitle(longRepoUrl)).toBeNull();
  });

  it("reveals the start/due date pickers from the ⋯ overflow menu", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    // Dates are collapsed behind the overflow by default (progressive disclosure).
    expect(screen.queryByRole("button", { name: "Start date" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Due date" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Set start date/ }));
    expect(screen.getByRole("button", { name: "Start date" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Set due date/ }));
    expect(screen.getByRole("button", { name: "Due date" })).toBeInTheDocument();
  });

  // A project that tracks one delivery line has to be able to say so while it
  // is being created — otherwise its very first tasks start from the default
  // branch and the setting is something to discover afterwards.
  it("attaches a repository with the checkout ref typed beside it", async () => {
    createProjectMock.mockClear();
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText(/project title/i), "Release 2026-09");
    await user.type(
      screen.getByPlaceholderText(/github\.com\/owner\/repo/i),
      apiRepoUrl,
    );
    await user.type(
      screen.getByLabelText(/starting branch/i),
      "release/2026-09",
    );
    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.click(screen.getByRole("button", { name: /^create project$/i }));

    expect(createProjectMock).toHaveBeenCalledTimes(1);
    const payload = createProjectMock.mock.calls[0]?.[0] as {
      resources?: Array<{ resource_type: string; resource_ref: Record<string, unknown> }>;
    };
    expect(payload.resources).toEqual([
      {
        resource_type: "github_repo",
        resource_ref: { url: apiRepoUrl, ref: "release/2026-09" },
      },
    ]);
  });

  it("omits the ref key for a repository left on its default branch", async () => {
    createProjectMock.mockClear();
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText(/project title/i), "Plain project");
    await user.type(
      screen.getByPlaceholderText(/github\.com\/owner\/repo/i),
      apiRepoUrl,
    );
    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.click(screen.getByRole("button", { name: /^create project$/i }));

    const payload = createProjectMock.mock.calls[0]?.[0] as {
      resources?: Array<{ resource_ref: Record<string, unknown> }>;
    };
    expect(payload.resources?.[0]?.resource_ref).toEqual({ url: apiRepoUrl });
  });

  // Regression: the submit gate only ever checked the custom-URL field, so a
  // branch edited on an ALREADY SELECTED repo could carry a commit id past a
  // visible inline error. The server accepts SHAs by design, so nothing
  // downstream would have caught it.
  it("refuses to create while any selected repo's branch is rejected", async () => {
    createProjectMock.mockClear();
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText(/project title/i), "Bad branch");
    await user.click(
      screen.getByRole("button", { name: (name) => name.includes(apiRepoUrl) }),
    );

    // The per-repo branch editor, reached from the selected-repo row.
    await user.click(screen.getByRole("button", { name: /default branch/i }));
    await user.type(
      screen.getByLabelText(/starting branch/i),
      "5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c",
    );
    expect(screen.getByText(/commit, not a branch/i)).toBeTruthy();

    const create = screen.getByRole("button", {
      name: /^create project$/i,
    }) as HTMLButtonElement;
    expect(create.disabled).toBe(true);
    await user.click(create);
    expect(createProjectMock).not.toHaveBeenCalled();
  });

  // The same gate has to hold for the keyboard path — TitleEditor's onSubmit
  // calls handleSubmit directly, bypassing the button's disabled state.
  it("refuses on keyboard submit too, not just the disabled button", async () => {
    createProjectMock.mockClear();
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText(/project title/i), "Bad branch");
    await user.click(
      screen.getByRole("button", { name: (name) => name.includes(apiRepoUrl) }),
    );
    await user.click(screen.getByRole("button", { name: /default branch/i }));
    await user.type(
      screen.getByLabelText(/starting branch/i),
      "5e0b1cfa0a7d6a1a0f4b3f2e1d0c9b8a7f6e5d4c",
    );

    fireEvent.keyDown(screen.getByPlaceholderText(/project title/i), { key: "Enter" });
    expect(createProjectMock).not.toHaveBeenCalled();
  });

  // Regression: the /tree/ split was gated on the branch field being empty, so
  // a SECOND pasted browse URL was stored whole — a clone target that does not
  // exist. Normalising the URL is unconditional; only the branch value is a
  // question of whether to overwrite.
  it("normalises a pasted browse URL even when a branch is already filled in", async () => {
    createProjectMock.mockClear();
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    await user.type(screen.getByPlaceholderText(/project title/i), "Second paste");
    const urlField = screen.getByPlaceholderText(/github\.com\/owner\/repo/i);
    await user.clear(urlField);
    await user.paste("https://github.com/multica-ai/one/tree/release/2026-09");
    expect((urlField as HTMLInputElement).value).toBe("https://github.com/multica-ai/one");
    expect((screen.getByLabelText(/starting branch/i) as HTMLInputElement).value).toBe(
      "release/2026-09",
    );

    // Changing your mind about which repo: the whole pair is replaced.
    await user.clear(urlField);
    await user.paste("https://github.com/multica-ai/two/tree/main");
    expect((urlField as HTMLInputElement).value).toBe("https://github.com/multica-ai/two");
    expect((screen.getByLabelText(/starting branch/i) as HTMLInputElement).value).toBe("main");

    await user.click(screen.getByRole("button", { name: /^add$/i }));
    await user.click(screen.getByRole("button", { name: /^create project$/i }));

    const payload = createProjectMock.mock.calls[0]?.[0] as {
      resources?: Array<{ resource_ref: Record<string, unknown> }>;
    };
    expect(payload.resources?.[0]?.resource_ref).toEqual({
      url: "https://github.com/multica-ai/two",
      ref: "main",
    });
  });

  it("filters workspace repositories by search text", async () => {
    const user = userEvent.setup();

    renderWithI18n(<CreateProjectModal onClose={vi.fn()} />);

    const repoSearchInput = screen.getByRole("textbox", { name: "Search repositories..." });

    await user.type(repoSearchInput, "api");

    expect(
      screen.getByRole("button", { name: (name) => name.includes(apiRepoUrl) }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: (name) => name.includes(webRepoUrl) }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: (name) => name.includes(longRepoUrl) }),
    ).not.toBeInTheDocument();

    await user.clear(repoSearchInput);
    await user.type(repoSearchInput, "no-match");

    expect(screen.getByText("No repositories match your search.")).toBeInTheDocument();
  });
});
