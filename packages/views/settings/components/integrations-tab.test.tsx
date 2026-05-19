import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogCancel: ({ children }: { children: ReactNode }) => <button>{children}</button>,
  AlertDialogAction: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <button onClick={onClick}>{children}</button>
  ),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: vi.fn(),
  useQueryClient: vi.fn(() => ({ invalidateQueries: vi.fn() })),
  useMutation: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listChannelBindings: vi.fn(),
    listProjects: vi.fn(),
    createChannelBinding: vi.fn(),
    deleteChannelBinding: vi.fn(),
    setPrimaryChannelBinding: vi.fn(),
    updateChannelBinding: vi.fn(),
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: vi.fn(() => ({ user: { id: "user-1", name: "Test User" } })),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: vi.fn(() => "ws-1"),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: vi.fn(() => ({ userId: "user-1", role: "admin", member: null, isLoading: false })),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: vi.fn(() => ({ id: "ws-1", name: "Test Workspace" })),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { IntegrationsTab } from "./integrations-tab";

const defaultUseQuery = (opts: { queryKey?: readonly unknown[] }) => {
  const key = opts.queryKey ?? [];
  if (key[0] === "channel-providers") {
    return { data: { providers: [] }, isLoading: false };
  }
  if (key[0] === "channel-connections") {
    return { data: { connections: [], can_manage: false }, isLoading: false };
  }
  if (key[0] === "workspaces" && key[2] === "agents") {
    return { data: [], isLoading: false };
  }
  if (key[0] === "settings" && key[1] === "integrations" && key[3] === "projects") {
    return { data: { projects: [] }, isLoading: false };
  }
  if (key[0] === "workspaces" && key[2] === "channel-bindings") {
    return { data: { bindings: [] }, isLoading: false };
  }
  return { data: undefined, isLoading: false };
};

function mockBindings(bindings: unknown[]) {
  (useQuery as ReturnType<typeof vi.fn>).mockImplementation((opts: { queryKey?: readonly unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (key[0] === "workspaces" && key[2] === "channel-bindings") {
      return { data: { bindings }, isLoading: false };
    }
    return defaultUseQuery(opts);
  });
}

describe("IntegrationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useQuery as ReturnType<typeof vi.fn>).mockImplementation(defaultUseQuery);
  });

  it("renders empty state when no bindings", () => {
    (useMutation as ReturnType<typeof vi.fn>).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    });

    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("No integrations yet.")).toBeInTheDocument();
  });

  it("renders binding list with primary badge", () => {
    mockBindings([
      {
        id: "bind-1",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_xxx",
        external_chat_name: "Test Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: true,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
    ]);
    (useMutation as ReturnType<typeof vi.fn>).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    });

    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("Test Group")).toBeInTheDocument();
    expect(screen.getByText("Primary")).toBeInTheDocument();
  });

  it("shows 'Set as Primary' button for non-primary binding", () => {
    mockBindings([
      {
        id: "bind-1",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_xxx",
        external_chat_name: "Test Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: true,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
      {
        id: "bind-2",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_yyy",
        external_chat_name: "Second Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: false,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
    ]);
    (useMutation as ReturnType<typeof vi.fn>).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    });

    renderWithI18n(<IntegrationsTab />);
    expect(screen.getByText("Second Group")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Set as Primary" })).toBeInTheDocument();
  });

  it("calls setPrimary when 'Set as Primary' is clicked", async () => {
    const setPrimaryMock = vi.fn().mockResolvedValue({});
    mockBindings([
      {
        id: "bind-1",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_xxx",
        external_chat_name: "Primary Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: true,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
      {
        id: "bind-2",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_yyy",
        external_chat_name: "Second Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: false,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
    ]);
    (useMutation as ReturnType<typeof vi.fn>).mockImplementation((opts: { mutationFn?: (vars: unknown) => Promise<unknown> }) => ({
      mutate: (vars: unknown, callbacks?: { onSettled?: () => void }) => {
        void opts?.mutationFn?.(vars).finally(() => callbacks?.onSettled?.());
      },
      mutateAsync: opts?.mutationFn ?? vi.fn(),
      isPending: false,
    }));
    (api.setPrimaryChannelBinding as ReturnType<typeof vi.fn>).mockImplementation(setPrimaryMock);

    const user = userEvent.setup();
    renderWithI18n(<IntegrationsTab />);

    const btn = screen.getByRole("button", { name: "Set as Primary" });
    await user.click(btn);

    await waitFor(() => {
      expect(setPrimaryMock).toHaveBeenCalledWith("ws-1", "bind-2", { is_primary: true });
    });
  });

  it("shows unbind confirmation dialog when unbind is clicked", async () => {
    const deleteMock = vi.fn().mockResolvedValue({});
    mockBindings([
      {
        id: "bind-1",
        provider: "feishu",
        connection_id: "feishu",
        external_chat_id: "oc_xxx",
        external_chat_name: "Test Group",
        chat_type: "group",
        listen_mode: "mentions",
        is_primary: true,
        bound_by_user_id: "user-1",
        created_at: "2026-05-06T00:00:00Z",
      },
    ]);
    (useMutation as ReturnType<typeof vi.fn>).mockImplementation((opts: { mutationFn?: (vars: unknown) => Promise<unknown> }) => ({
      mutateAsync: opts?.mutationFn ?? vi.fn(),
      isPending: false,
    }));
    (api.deleteChannelBinding as ReturnType<typeof vi.fn>).mockImplementation(deleteMock);

    const user = userEvent.setup();
    renderWithI18n(<IntegrationsTab />);

    const unbindBtn = screen.getByRole("button", { name: "Unbind" });
    await user.click(unbindBtn);

    expect(screen.getByText("Unbind Test Group?")).toBeInTheDocument();

    const confirmBtn = screen.getByRole("button", { name: "Confirm" });
    await user.click(confirmBtn);

    await waitFor(() => {
      expect(deleteMock).toHaveBeenCalledWith("ws-1", "bind-1");
    });
  });

  it("saves binding settings from dialog via updateChannelBinding", async () => {
    const updateMock = vi.fn().mockResolvedValue({});
    (api.updateChannelBinding as ReturnType<typeof vi.fn>).mockImplementation(updateMock);
    const bindingRow = {
      id: "bind-1",
      provider: "feishu",
      connection_id: "feishu-conn",
      external_chat_id: "oc_xxx",
      external_chat_name: "Test Group",
      chat_type: "group",
      listen_mode: "mentions",
      is_primary: true,
      bound_by_user_id: "user-1",
      created_at: "2026-05-06T00:00:00Z",
      default_project_id: null,
      agent_id: null,
    };
    (useQuery as ReturnType<typeof vi.fn>).mockImplementation((opts: { queryKey?: readonly unknown[] }) => {
      const key = opts.queryKey ?? [];
      if (key[0] === "workspaces" && key[2] === "channel-bindings") {
        return { data: { bindings: [bindingRow] }, isLoading: false };
      }
      if (key[0] === "workspaces" && key[2] === "agents") {
        return { data: [{ id: "agent-1", name: "Agent One", archived_at: null }], isLoading: false };
      }
      if (key[0] === "settings" && key[1] === "integrations" && key[3] === "projects") {
        return { data: { projects: [{ id: "proj-1", title: "Project One" }] }, isLoading: false };
      }
      return defaultUseQuery(opts);
    });
    (useMutation as ReturnType<typeof vi.fn>).mockImplementation((opts: { mutationFn?: (vars: unknown) => Promise<unknown> }) => ({
      mutate: (vars: unknown, callbacks?: { onSettled?: () => void }) => {
        void opts?.mutationFn?.(vars).finally(() => callbacks?.onSettled?.());
      },
      mutateAsync: opts?.mutationFn ?? vi.fn(),
      isPending: false,
    }));

    const user = userEvent.setup();
    renderWithI18n(<IntegrationsTab />);

    await user.click(screen.getByRole("button", { name: "Edit" }));

    await user.selectOptions(screen.getByLabelText(/Default project/i), "proj-1");
    await user.selectOptions(screen.getByLabelText(/Listen scope/i), "all");
    await user.selectOptions(screen.getByLabelText(/Agent \(optional\)/i), "agent-1");

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalledWith("ws-1", "bind-1", {
        default_project_id: "proj-1",
        listen_mode: "all",
        agent_id: "agent-1",
      });
    });
  });
});
