import { type ChangeEvent, type ReactNode, type Ref, forwardRef, useImperativeHandle, useState } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { CreateProjectModal } from "./create-project";

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateProject = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useCreateProject: () => ({ mutateAsync: mockCreateProject }),
}));

vi.mock("@multica/core/projects/config", () => ({
  PROJECT_STATUS_CONFIG: { planned: { label: "Planned", dotColor: "bg-gray-400" } },
  PROJECT_STATUS_ORDER: ["planned"],
  PROJECT_PRIORITY_CONFIG: { none: { label: "No Priority" } },
  PROJECT_PRIORITY_ORDER: ["none"],
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Workspace" }),
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/ws-test/projects/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lead" }),
}));

vi.mock("@multica/ui/lib/utils", () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(" "),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TooltipTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: ({
    value,
    onChange,
    placeholder,
    disabled,
    id,
    "aria-describedby": ariaDescribedBy,
  }: {
    value?: string;
    onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
    placeholder?: string;
    disabled?: boolean;
    id?: string;
    "aria-describedby"?: string;
  }) => (
    <input
      id={id}
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      disabled={disabled}
      aria-describedby={ariaDescribedBy}
    />
  ),
}));

vi.mock("@multica/ui/components/ui/checkbox", () => ({
  Checkbox: ({
    checked,
    onCheckedChange,
    id,
  }: {
    checked?: boolean;
    onCheckedChange?: (checked: boolean) => void;
    id?: string;
  }) => (
    <input
      id={id}
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange?.(e.target.checked)}
    />
  ),
}));

vi.mock("@multica/ui/components/common/emoji-picker", () => ({
  EmojiPicker: () => null,
}));

vi.mock("../issues/components/priority-icon", () => ({
  PriorityIcon: () => <span>priority</span>,
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span>avatar</span>,
}));

vi.mock("../editor", () => {
  const ContentEditor = forwardRef((_props: unknown, ref: Ref<{ getMarkdown: () => string }>) => {
    useImperativeHandle(ref, () => ({
      getMarkdown: () => "",
    }));
    return <div data-testid="content-editor" />;
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    ContentEditor,
    TitleEditor: ({ placeholder, onChange, onSubmit }: any) => {
      const [value, setValue] = useState("");
      return (
        <input
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            setValue(e.target.value);
            onChange?.(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmit?.();
          }}
        />
      );
    },
  };
});

describe("CreateProjectModal local_path", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateProject.mockResolvedValue({ id: "project-1" });
  });

  it("does not send local_path when create-from-folder is disabled", async () => {
    render(<CreateProjectModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText("Project title"), {
      target: { value: "Project A" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Project" }));

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalledTimes(1));
    expect(mockCreateProject).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Project A",
        local_path: undefined,
      }),
    );
  });

  it("disables submit when create-from-folder is enabled and path is empty", () => {
    render(<CreateProjectModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText("Project title"), {
      target: { value: "Project B" },
    });
    fireEvent.click(screen.getByLabelText(/create from existing folder/i));

    expect(screen.getByRole("button", { name: "Create Project" })).toBeDisabled();
  });

  it("trims and sends local_path when create-from-folder is enabled", async () => {
    render(<CreateProjectModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText("Project title"), {
      target: { value: "Project C" },
    });
    fireEvent.click(screen.getByLabelText(/create from existing folder/i));
    fireEvent.change(screen.getByPlaceholderText("/home/user/projects/my-app"), {
      target: { value: "  /home/user/projects/project-c  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Project" }));

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalledTimes(1));
    expect(mockCreateProject).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Project C",
        local_path: "/home/user/projects/project-c",
      }),
    );
  });
});
