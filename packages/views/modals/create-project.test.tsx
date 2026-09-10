import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";

import { useProjectDraftStore } from "@multica/core/projects";
const { createProject, push } = vi.hoisted(() => ({ createProject: vi.fn(), push: vi.fn() }));
beforeEach(() => { useProjectDraftStore.getState().clearDraft(); createProject.mockReset().mockResolvedValue({ id: "new-project" }); push.mockClear(); });

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

vi.mock("@multica/core/projects/mutations", () => ({
  useCreateProject: () => ({ mutateAsync: createProject }),
}));

vi.mock("@multica/core/projects", async (importOriginal) => {
  const { useProjectDraftStore } = await importOriginal<typeof import("@multica/core/projects")>();
  return { useProjectDraftStore };
});

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
  squadListOptions: () => ({ queryKey: ["squads"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: vi.fn() }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push }),
}));

vi.mock("../editor", () => {
  const ContentEditor = React.forwardRef<{ getMarkdown: () => string }, { placeholder?: string }>(
    ({ placeholder }, ref) => {
      React.useImperativeHandle(ref, () => ({ getMarkdown: () => "Project goal" }));
      return <textarea placeholder={placeholder} />;
    },
  );
  ContentEditor.displayName = "ContentEditor";

  return {
    ContentEditor,
    TitleEditor: ({
      placeholder,
      onChange,
    }: {
      placeholder?: string;
      onChange?: (value: string) => void;
    }) => <input placeholder={placeholder} onChange={(e) => onChange?.(e.target.value)} />,
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


it("waits for workflow setup, submits project and workflow together, and keeps the draft on failure", async () => {
  const user = userEvent.setup(); const onClose = vi.fn();
  createProject.mockRejectedValueOnce(new Error("Could not save project"));
  renderWithI18n(<CreateProjectModal onClose={onClose} />);
  await user.type(screen.getByPlaceholderText("Project title"), "Launch");
  await user.click(screen.getByRole("button", { name: "Next: configure workflow" }));
  expect(createProject).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: /Start from scratch/ }));
  await user.click(screen.getByRole("button", { name: "Change workflow source" }));
  expect(screen.getByRole("button", { name: "Create Project" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Back" }));
  await user.click(screen.getByRole("button", { name: "Next: configure workflow" }));
  expect(screen.getByRole("button", { name: "Create Project" })).toBeDisabled();
  expect(screen.getByRole("button", { name: /Start from scratch/ })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await user.click(screen.getByRole("button", { name: "Create Project" }));
  await waitFor(() => expect(createProject).toHaveBeenCalledTimes(1));
  expect(createProject).toHaveBeenCalledWith(expect.objectContaining({ title: "Launch", description: "Project goal", issue_workflow: expect.objectContaining({ api_version: 1, initial_status: "status_1", statuses: expect.arrayContaining([expect.objectContaining({ name: "Ready" }), expect.objectContaining({ name: "Done" })]) }) }));
  expect(onClose).not.toHaveBeenCalled();
  expect(useProjectDraftStore.getState().draft.workflow?.statuses).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: "Create Project" }));
  await waitFor(() => expect(push).toHaveBeenCalledWith("/test-workspace/projects/new-project?view=workflow"));
  expect(onClose).toHaveBeenCalledOnce();
  expect(useProjectDraftStore.getState().draft.workflow).toBeUndefined();
});
