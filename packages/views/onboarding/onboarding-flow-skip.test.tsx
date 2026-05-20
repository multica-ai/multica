/**
 * Integration test for tc-052: clicking "Skip for now" on the runtime
 * step must call bootstrapNoRuntimeOnboarding and complete onboarding.
 *
 * This test exercises the full OnboardingFlow component (not just
 * StepPlatformFork in isolation) to catch closure/ref bugs where
 * `workspace` might be stale inside `handleRuntimeNext`.
 *
 * Strategy: drive through Welcome → questionnaire → workspace → runtime skip.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

// --- Hoisted mocks ---
const mocks = vi.hoisted(() => ({
  bootstrapNoRuntime: vi.fn<
    (wsId: string) => Promise<{ workspace_id: string; issue_id: string }>
  >(),
  bootstrapRuntime: vi.fn(),
  completeOnboarding: vi.fn(),
  saveQuestionnaire: vi.fn().mockResolvedValue(undefined),
  refreshMe: vi.fn(),
  user: {
    id: "user_1",
    name: "Test User",
    email: "test@multica.com",
    onboarded_at: null as string | null,
    onboarding_questionnaire: {},
  },
  workspaces: [] as Array<{ id: string; slug: string; name: string }>,
}));

vi.mock("@multica/core/onboarding", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/onboarding")>();
  return {
    ...actual,
    bootstrapNoRuntimeOnboarding: mocks.bootstrapNoRuntime,
    bootstrapRuntimeOnboarding: mocks.bootstrapRuntime,
    completeOnboarding: mocks.completeOnboarding,
    saveQuestionnaire: mocks.saveQuestionnaire,
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) =>
    selector({
      user: mocks.user,
      isLoading: false,
      refreshMe: mocks.refreshMe,
      setUser: vi.fn(),
    }),
}));

vi.mock("@multica/core/analytics", () => ({
  captureEvent: vi.fn(),
  captureDownloadIntent: vi.fn(),
  setPersonProperties: vi.fn(),
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspaces", "list"],
    queryFn: () => Promise.resolve(mocks.workspaces),
  }),
  workspaceKeys: {
    list: () => ["workspaces", "list"],
    agents: (wsId: string) => ["workspaces", wsId, "agents"],
  },
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueKeys: {
    all: (wsId: string) => ["issues", wsId],
  },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({
    mutate: (_data: unknown, opts: { onSuccess?: (ws: unknown) => void }) => {
      const ws = { id: "ws_new", slug: "test-ws", name: "Test Workspace" };
      mocks.workspaces = [ws];
      opts.onSuccess?.(ws);
    },
    isPending: false,
  }),
}));

vi.mock("./components/use-runtime-picker", () => ({
  useRuntimePicker: () => ({
    runtimes: [],
    selected: null,
    selectedId: null,
    setSelectedId: vi.fn(),
    hasRuntimes: false,
  }),
}));

vi.mock("../workspace/slug", () => ({
  WORKSPACE_SLUG_REGEX: /^[a-z0-9-]+$/,
  isWorkspaceSlugConflict: () => false,
  nameToWorkspaceSlug: (name: string) => name.toLowerCase().replace(/\s+/g, "-"),
}));

vi.mock("@multica/core/paths", () => ({
  isReservedSlug: () => false,
}));

vi.mock("@multica/core/utils", () => ({
  isImeComposing: () => false,
}));

import { OnboardingFlow } from "./onboarding-flow";

function createWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

describe("OnboardingFlow — runtime skip (tc-052)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.workspaces = [];
    mocks.user.onboarded_at = null;
    mocks.bootstrapNoRuntime.mockResolvedValue({
      workspace_id: "ws_new",
      issue_id: "issue_guide",
    });
  });

  it("clicking Skip on the runtime step calls bootstrapNoRuntimeOnboarding with workspace.id", async () => {
    const onComplete = vi.fn();
    const user = userEvent.setup();

    render(
      <OnboardingFlow
        onComplete={onComplete}
        runtimeInstructions={<div>CLI instructions</div>}
      />,
      { wrapper: createWrapper() },
    );

    // Step 0: Welcome — click "Continue on web" (isWeb=true because runtimeInstructions is set)
    const continueBtn = await screen.findByRole("button", { name: /continue on web/i });
    await user.click(continueBtn);

    // Step 1-3: Source / Role / Use-case — click "Skip" on each
    for (let i = 0; i < 3; i++) {
      const skipBtn = await screen.findByRole("button", { name: /^skip$/i });
      await user.click(skipBtn);
    }

    // Step 4: Workspace — type name + create
    const nameInput = await screen.findByLabelText(/name/i);
    await user.clear(nameInput);
    await user.type(nameInput, "Test Workspace");
    const createBtn = await screen.findByRole("button", { name: /create|continue/i });
    await user.click(createBtn);

    // Step 5: Runtime (web) — StepPlatformFork renders with "Skip for now"
    const skipForNow = await screen.findByRole("button", { name: /skip for now/i });
    expect(skipForNow).toBeEnabled();
    await user.click(skipForNow);

    // Assert: bootstrapNoRuntimeOnboarding was called with the workspace id
    await waitFor(() => {
      expect(mocks.bootstrapNoRuntime).toHaveBeenCalledTimes(1);
      expect(mocks.bootstrapNoRuntime).toHaveBeenCalledWith("ws_new");
    });

    // Assert: onComplete was called with workspace + issue
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith(
        expect.objectContaining({ id: "ws_new" }),
        "issue_guide",
      );
    });
  });
});
