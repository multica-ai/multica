import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { authStateRef, mockCreateChannelBinding, mockCreateChannelUserBinding, mockGetBindPreview, mockListProjects, mockListAgents, routerPush, routerReplace, searchParamsState } =
  vi.hoisted(() => ({
    authStateRef: {
      state: {
        user: { id: "user-1", email: "test@multica.ai" },
        isLoading: false,
      },
    },
    mockCreateChannelBinding: vi.fn(),
    mockCreateChannelUserBinding: vi.fn(),
    mockGetBindPreview: vi.fn(),
    mockListProjects: vi.fn(),
    mockListAgents: vi.fn(),
    routerPush: vi.fn(),
    routerReplace: vi.fn(),
    searchParamsState: { params: new URLSearchParams({ token: "bind-token", provider: "feishu" }) },
  }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPush, replace: routerReplace }),
  useSearchParams: () => searchParamsState.params,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: typeof authStateRef.state) => unknown) => selector(authStateRef.state),
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: (options: { queryKey?: unknown[] }) => {
    if (options.queryKey?.[0] === "channel-bind-token") {
      return {
        data: mockGetBindPreview(),
        isLoading: false,
      };
    }
    if (options.queryKey?.[0] === "workspaces" && options.queryKey?.[1] === "list") {
      return {
        data: [{ id: "ws-1", slug: "acme", name: "Acme" }],
        isLoading: false,
      };
    }
    if (options.queryKey?.[0] === "channel-bind-projects") {
      return {
        data: mockListProjects(),
        isLoading: false,
      };
    }
    if (options.queryKey?.[0] === "workspaces" && options.queryKey?.[2] === "agents") {
      return {
        data: mockListAgents(),
        isLoading: false,
      };
    }
    return { data: [], isLoading: false };
  },
}));

const userPreview = () => ({
          kind: "user",
          provider: "feishu",
          connection_id: "feishu",
          connection_display_name: "Feishu",
          external_chat_id: null,
          external_chat_name: null,
          expires_at: "2026-01-01T00:00:00Z",
});

vi.mock("@multica/core/api", () => {
  class ApiError extends Error {
    status: number;
    statusText: string;
    body: unknown;

    constructor(message: string, status: number, statusText = "", body?: unknown) {
      super(message);
      this.status = status;
      this.statusText = statusText;
      this.body = body;
    }
  }

  return {
    ApiError,
    api: {
      createChannelBinding: mockCreateChannelBinding,
      createChannelUserBinding: mockCreateChannelUserBinding,
      getChannelBindTokenPreview: mockGetBindPreview,
      listProjects: mockListProjects,
      listAgents: mockListAgents,
    },
  };
});

import BindPage from "./page";

describe("BindPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    authStateRef.state.user = { id: "user-1", email: "test@multica.ai" };
    authStateRef.state.isLoading = false;
    searchParamsState.params = new URLSearchParams({ token: "bind-token", provider: "feishu" });
    mockGetBindPreview.mockReturnValue(userPreview());
    mockListProjects.mockReturnValue({ projects: [] });
    mockListAgents.mockReturnValue([]);
    mockCreateChannelUserBinding.mockResolvedValue({
      provider: "feishu",
      external_user_id: "ou_1",
      user_id: "user-1",
    });
    mockCreateChannelBinding.mockResolvedValue({});
  });

  it("shows success after the binding request resolves", async () => {
    render(<BindPage />);

    expect(screen.getByText("正在绑定 Feishu 账号")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText("绑定完成")).toBeInTheDocument();
    });
    expect(mockCreateChannelUserBinding).toHaveBeenCalledWith({
      token: "bind-token",
      provider: "feishu",
      connection_id: "feishu",
    });
  });

  it("binds chat with a selected default project", async () => {
    mockGetBindPreview.mockReturnValue({
      ...userPreview(),
      kind: "chat",
      external_chat_id: "oc_1",
      external_chat_name: "Team",
    });
    mockListProjects.mockReturnValue({
      projects: [{ id: "project-1", title: "Launch" }],
    });
    mockListAgents.mockReturnValue([]);

    const user = userEvent.setup();
    render(<BindPage />);

    await user.click(screen.getByRole("button", { name: "Acme" }));
    await user.selectOptions(screen.getByLabelText("默认项目"), "project-1");
    await user.click(screen.getByRole("button", { name: "完成绑定" }));

    await waitFor(() => {
      expect(mockCreateChannelBinding).toHaveBeenCalledWith("ws-1", {
        token: "bind-token",
        provider: "feishu",
        connection_id: "feishu",
        default_project_id: "project-1",
        listen_mode: "mentions",
      });
    });
    expect(routerPush).toHaveBeenCalledWith("/acme/settings");
  });

  it("binds chat with optional agent id", async () => {
    mockGetBindPreview.mockReturnValue({
      ...userPreview(),
      kind: "chat",
      external_chat_id: "oc_1",
      external_chat_name: "Team",
    });
    mockListProjects.mockReturnValue({ projects: [] });
    mockListAgents.mockReturnValue([{ id: "agent-1", name: "Alpha", archived_at: null }]);

    const user = userEvent.setup();
    render(<BindPage />);

    await user.click(screen.getByRole("button", { name: "Acme" }));
    await user.selectOptions(screen.getByLabelText("指定 Agent（可选）"), "agent-1");
    await user.click(screen.getByRole("button", { name: "完成绑定" }));

    await waitFor(() => {
      expect(mockCreateChannelBinding).toHaveBeenCalledWith("ws-1", {
        token: "bind-token",
        provider: "feishu",
        connection_id: "feishu",
        default_project_id: null,
        listen_mode: "mentions",
        agent_id: "agent-1",
      });
    });
  });
});
