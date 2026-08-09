import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { useWelcomeStore } from "@multica/core/onboarding";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../navigation";
import { WelcomeAfterOnboarding } from "./welcome-after-onboarding";

const mockUser = {
  id: "user-1",
  name: "Test",
  email: "test@multica.ai",
  avatar_url: null,
  onboarded_at: "2026-01-01T00:00:00Z",
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  created_at: "",
  updated_at: "",
};

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: { user: typeof mockUser }) => unknown) => {
      const state = { user: mockUser };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockUser }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

const mockCreateIssue = vi.fn();
const mockGetWorkspace = vi.fn();

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({
      id: "ws-1",
      slug: "test-ws",
      name: "Test WS",
    }),
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    createIssue: (...args: unknown[]) => mockCreateIssue(...args),
    getWorkspace: (...args: unknown[]) => mockGetWorkspace(...args),
  },
}));

const mockPush = vi.fn();
const navigationAdapter: NavigationAdapter = {
  push: (path: string) => mockPush(path),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/test",
  searchParams: new URLSearchParams(),
  getShareableUrl: (path: string) => `https://test.local${path}`,
};

function TestProviders({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(
    ["workspaces", "list"],
    [{ id: "ws-1", slug: "test-ws" }],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <I18nProvider
        locale="en"
        resources={{
          en: { common: enCommon, onboarding: enOnboarding },
        }}
      >
        <NavigationProvider value={navigationAdapter}>
          {children}
        </NavigationProvider>
      </I18nProvider>
    </QueryClientProvider>
  );
}

function renderWelcome() {
  return render(<WelcomeAfterOnboarding />, { wrapper: TestProviders });
}

beforeEach(() => {
  mockCreateIssue.mockReset();
  mockGetWorkspace.mockReset();
  mockPush.mockReset();
  useWelcomeStore.getState().reset();
});

describe("WelcomeAfterOnboarding", () => {
  it("renders nothing when no skip signal is present", () => {
    const { container } = renderWelcome();
    expect(container.firstChild).toBeNull();
    expect(mockCreateIssue).not.toHaveBeenCalled();
  });

  it("does not seed issues for a different workspace", () => {
    useWelcomeStore.getState().set({
      workspaceId: "ws-2",
      choice: "skip",
    });

    const { container } = renderWelcome();
    expect(container.firstChild).toBeNull();
    expect(mockCreateIssue).not.toHaveBeenCalled();
  });

  it("provisions the no-runtime guide, then opens the completion modal", async () => {
    mockCreateIssue.mockResolvedValueOnce({
      id: "issue-install",
      identifier: "MUL-1",
      workspace_id: "ws-1",
    });
    useWelcomeStore.getState().set({
      workspaceId: "ws-1",
      choice: "skip",
    });

    renderWelcome();

    expect(screen.getByText(/Setting up your workspace/i)).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText(/Welcome to Multica/i)).toBeInTheDocument();
    });

    expect(mockCreateIssue).toHaveBeenCalledTimes(1);
    expect(mockCreateIssue.mock.calls[0]![0]).toMatchObject({
      title: "Connect a runtime to start with Mika",
      status: "in_progress",
      assignee_type: "member",
      assignee_id: "user-1",
    });
    expect(mockCreateIssue.mock.calls[0]![0].description).toContain(
      "Start with Mika",
    );

    fireEvent.click(screen.getByRole("button", { name: /got it/i }));
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/test-ws/issues/issue-install");
    });
  });

  // The surface is one-shot and the signal is not persisted, so dismissing on
  // failure used to be terminal: no guide issue, no message, nothing to
  // reload. A failure has to stay on screen and offer a way forward.
  it("offers a retry instead of dismissing when guide provisioning fails", async () => {
    mockCreateIssue.mockRejectedValueOnce(new Error("network down"));
    useWelcomeStore.getState().set({
      workspaceId: "ws-1",
      choice: "skip",
    });

    renderWelcome();

    expect(
      await screen.findByRole("button", { name: /try again/i }),
    ).toBeInTheDocument();
    expect(useWelcomeStore.getState().dismissed).toBe(false);
  });

  it("provisions the guide issue when the member retries", async () => {
    mockCreateIssue
      .mockRejectedValueOnce(new Error("network down"))
      .mockResolvedValueOnce({ id: "issue-install", workspace_id: "ws-1" });
    useWelcomeStore.getState().set({
      workspaceId: "ws-1",
      choice: "skip",
    });

    renderWelcome();

    fireEvent.click(await screen.findByRole("button", { name: /try again/i }));

    expect(await screen.findByText(/Welcome to Multica/i)).toBeInTheDocument();
    expect(mockCreateIssue).toHaveBeenCalledTimes(2);
  });

  it("dismisses only when the member chooses to", async () => {
    mockCreateIssue.mockRejectedValueOnce(new Error("network down"));
    useWelcomeStore.getState().set({
      workspaceId: "ws-1",
      choice: "skip",
    });

    renderWelcome();

    fireEvent.click(await screen.findByRole("button", { name: /not now/i }));

    await waitFor(() => {
      expect(useWelcomeStore.getState().dismissed).toBe(true);
    });
  });
});
