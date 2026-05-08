import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockQuickCreateIssue = vi.hoisted(() => vi.fn());
const mockSetLastAgentId = vi.hoisted(() => vi.fn());
const mockSetPrompt = vi.hoisted(() => vi.fn());
const mockClearPrompt = vi.hoisted(() => vi.fn());
const mockSetKeepOpen = vi.hoisted(() => vi.fn());
const mockSetLastMode = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());

const mockQuickCreateStore = {
  lastAgentId: null as string | null,
  setLastAgentId: mockSetLastAgentId,
  prompt: "Persisted draft prompt",
  setPrompt: mockSetPrompt,
  clearPrompt: mockClearPrompt,
  keepOpen: false,
  setKeepOpen: mockSetKeepOpen,
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    switch (queryKey[0]) {
      case "members":
        return { data: [{ user_id: "user-1", role: "admin" }] };
      case "agents":
        return {
          data: [{ id: "agent-1", name: "Bohan", archived_at: null, runtime_id: "runtime-1" }],
        };
      case "runtimes":
        return { data: [{ id: "runtime-1", metadata: { cli_version: "1.2.3" } }] };
      default:
        return { data: [] };
    }
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    quickCreateIssue: mockQuickCreateIssue,
  },
  ApiError: class ApiError extends Error {
    body?: unknown;
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Test Workspace" }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/issues/stores/quick-create-store", () => ({
  useQuickCreateStore: (selector?: (state: typeof mockQuickCreateStore) => unknown) =>
    (selector ? selector(mockQuickCreateStore) : mockQuickCreateStore),
}));

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: (selector?: (state: { setLastMode: typeof mockSetLastMode }) => unknown) =>
    (selector ? selector({ setLastMode: mockSetLastMode }) : { setLastMode: mockSetLastMode }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (state: { user: { id: string } }) => unknown) =>
    (selector ? selector({ user: { id: "user-1" } }) : { user: { id: "user-1" } }),
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  checkQuickCreateCliVersion: () => ({ state: "ok", min: "1.0.0" }),
  readRuntimeCliVersion: () => "1.2.3",
  MIN_QUICK_CREATE_CLI_VERSION: "1.0.0",
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ uploadWithToast: vi.fn(), uploading: false }),
}));

vi.mock("../issues/components/pickers/assignee-picker", () => ({
  canAssignAgent: () => true,
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../issues/components", () => ({
  PriorityPicker: () => <div data-testid="priority-picker" />,
  DueDatePicker: () => <div data-testid="due-date-picker" />,
}));

vi.mock("../projects/components/project-picker", () => ({
  ProjectPicker: () => <div data-testid="project-picker" />,
}));

vi.mock("../common/pill-button", () => ({
  PillButton: () => <div data-testid="pill-button" />,
}));

vi.mock("../editor", () => {
  const ContentEditor = forwardRef(({ defaultValue, onUpdate, onSubmit, placeholder }: any, ref: any) => {
    const valueRef = useRef(defaultValue || "");
    const [value, setValue] = useState(defaultValue || "");

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = "";
        setValue("");
      },
      uploadFile: vi.fn(),
      focus: vi.fn(),
    }));

    return (
      <textarea
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
            onSubmit?.();
          }
        }}
      />
    );
  });
  ContentEditor.displayName = "ContentEditor";

  return {
    ContentEditor,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
  };
});

vi.mock("@multica/ui/components/ui/dialog", () => ({
  DialogTitle: ({ children, className }: { children: ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>{children}</button>
  ),
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, disabled, onClick }: { children: ReactNode; disabled?: boolean; onClick?: () => void }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("@multica/ui/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange }: { checked: boolean; onCheckedChange: (v: boolean) => void }) => (
    <input
      aria-label="Create another"
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
    />
  ),
}));

vi.mock("@multica/ui/components/common/file-upload-button", () => ({
  FileUploadButton: () => <button type="button">Upload file</button>,
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
  },
}));

import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enModals from "../locales/en/modals.json";
import { AgentCreatePanel } from "./quick-create-issue";

const TEST_RESOURCES = { en: { common: enCommon, modals: enModals } };

function renderPanel(props: React.ComponentProps<typeof AgentCreatePanel>) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentCreatePanel {...props} />
    </I18nProvider>,
  );
}

describe("AgentCreatePanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuickCreateStore.lastAgentId = null;
    mockQuickCreateStore.prompt = "Persisted draft prompt";
    mockQuickCreateStore.keepOpen = false;
    mockQuickCreateIssue.mockResolvedValue(undefined);
    mockSetKeepOpen.mockImplementation((value: boolean) => {
      mockQuickCreateStore.keepOpen = value;
    });
  });

  it("loads the persisted prompt draft when no transient prompt is provided", () => {
    renderPanel({ onClose: vi.fn() });

    expect(
      screen.getByPlaceholderText(
        'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
      ),
    ).toHaveValue("Persisted draft prompt");
  });

  it("writes prompt changes back to the draft store and clears them after submit", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderPanel({ onClose });

    const editor = screen.getByPlaceholderText(
      'Tell the agent what to do, e.g. "let Bohan fix the inbox loading slowness in the Web project"',
    );

    await user.clear(editor);
    await user.type(editor, "New agent prompt");
    expect(mockSetPrompt).toHaveBeenLastCalledWith("New agent prompt");

    await user.click(screen.getByRole("button", { name: /^Create \(/i }));

    await waitFor(() => {
      expect(mockQuickCreateIssue).toHaveBeenCalledWith({
        agent_id: "agent-1",
        prompt: "New agent prompt",
      });
    });

    expect(mockSetLastAgentId).toHaveBeenCalledWith("agent-1");
    expect(mockClearPrompt).toHaveBeenCalled();
    expect(mockSetLastMode).toHaveBeenCalledWith("agent");
    expect(onClose).toHaveBeenCalled();
  });
});
