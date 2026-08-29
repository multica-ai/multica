import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";

const { localLogin, setToken, setAuthState } = vi.hoisted(() => ({
  localLogin: vi.fn(),
  setToken: vi.fn(),
  setAuthState: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { localLogin, localSetup: vi.fn(), setToken },
  ApiError: class ApiError extends Error {
    constructor(public readonly status: number) {
      super("api error");
    }
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(vi.fn(), { setState: setAuthState }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/views/auth", () => ({
  LoginPage: () => <div>Multica login</div>,
}));

vi.mock("@multica/views/platform", () => ({
  DragStrip: () => <div aria-hidden="true" />,
}));

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resource: unknown) => string) =>
      selector({
        lifeos: {
          title: "AI 星耀工作台",
          login_description: "使用本机专属账号登录",
          setup_description: "首次使用",
          username: "用户名",
          password: "密码",
          password_confirm: "再次输入密码",
          password_requirements: "密码要求",
          password_mismatch: "两次输入的密码不一致",
          invalid_credentials: "用户名或密码不正确",
          too_many_attempts: "尝试次数过多",
          setup_failed: "无法创建本机账号",
          submitting: "正在验证…",
          login: "进入工作台",
          create_login: "创建账号并进入",
        },
      }),
  }),
}));

import { DesktopLoginPage } from "./login";

describe("DesktopLoginPage in LifeOS", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    configStore.setState({ localAuthConfigured: true, localMode: true });
    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: {
        appInfo: { version: "test", os: "macos", flavor: "lifeos" },
      },
    });
  });

  it("logs in locally, stores the returned token, and hydrates the session", async () => {
    const session = {
      token: "local-token",
      user: { id: "user-1", name: "陈星耀" },
      workspace: { id: "workspace-1", slug: "lifeos", name: "LifeOS" },
    };
    localLogin.mockResolvedValue(session);
    const queryClient = new QueryClient();

    render(
      <QueryClientProvider client={queryClient}>
        <DesktopLoginPage />
      </QueryClientProvider>,
    );

    fireEvent.change(screen.getByLabelText("用户名"), {
      target: { value: "chairman" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Strong!LifeOS2026" },
    });
    fireEvent.click(screen.getByRole("button", { name: "进入工作台" }));

    await waitFor(() => {
      expect(localLogin).toHaveBeenCalledWith(
        "chairman",
        "Strong!LifeOS2026",
      );
    });
    expect(localStorage.getItem("multica_token")).toBe("local-token");
    expect(setToken).toHaveBeenCalledWith("local-token");
    expect(queryClient.getQueryData(["workspaces"])).toEqual([
      session.workspace,
    ]);
    expect(setAuthState).toHaveBeenCalledWith({
      user: session.user,
      isLoading: false,
    });
  });
});
