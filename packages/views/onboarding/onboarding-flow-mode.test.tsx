import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";
import enWorkspace from "../locales/en/workspace.json";

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (
    selector: (s: { workspaceCreationDisabled: boolean; daemonAppUrl: string }) => unknown,
  ) => selector({ workspaceCreationDisabled: false, daemonAppUrl: "" }),
}));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://multica.ai" },
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutate: vi.fn(), isPending: false }),
}));

const authState = vi.hoisted(() => ({
  questionnaire: {} as Record<string, unknown>,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) =>
      selector({
        user: { id: "u-1", name: "Ada", onboarding_questionnaire: authState.questionnaire },
      }),
    { getState: () => ({ user: { id: "u-1" } }) },
  ),
}));

vi.mock("./steps/step-agent-setup", () => ({
  StepAgentSetup: ({ wsSlug }: { wsSlug: string }) => <div>agent setup for {wsSlug}</div>,
}));

// Returning one workspace proves new-workspace mode does not offer to
// continue with it.
const workspaceState = vi.hoisted(() => ({
  workspaces: [{ id: "ws-1", name: "Existing", slug: "existing" }] as unknown[],
}));
vi.mock("@multica/core/workspace", () => {
  return {
    useWorkspaceList: () => ({ workspaces: workspaceState.workspaces, ready: true }),
  };
});

vi.mock("@multica/core/onboarding", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@multica/core/onboarding",
  );
  return actual;
});

import { OnboardingFlow } from "./onboarding-flow";

function renderFlow(props: Record<string, unknown>) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <OnboardingFlow onComplete={vi.fn()} {...props} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("OnboardingFlow — new-workspace mode", () => {
  it("starts at the workspace step instead of the product intro", () => {
    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });

    // The welcome screen teaches what Multica is; someone creating a second
    // workspace already knows, so the flow opens on naming it.
    expect(
      screen.getByRole("heading", { name: /Name your workspace/i }),
    ).toBeInTheDocument();
  });

  it("always creates a workspace rather than offering the existing one", () => {
    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });

    // First-run onboarding offers "Continue with {name}" when an abandoned
    // workspace exists. Doing that here would defeat the whole intent.
    expect(screen.queryByText(/Continue with/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Workspace name")).toBeInTheDocument();
  });

  it("still opens on the product intro in first-run mode", () => {
    authState.questionnaire = {};
    renderFlow({});

    expect(
      screen.queryByRole("heading", { name: /Name your workspace/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/agent setup for/)).not.toBeInTheDocument();
  });
});

describe("OnboardingFlow — resuming", () => {
  it("picks up at the agent step for someone who answered the questions and made a workspace", () => {
    // Started in the browser, now opening Desktop (or another computer).
    authState.questionnaire = { role: "engineer", use_case: ["ship_code"] };
    renderFlow({});

    expect(screen.getByText("agent setup for existing")).toBeInTheDocument();
  });

  it("opens on the product intro when the answers exist but no workspace does", async () => {
    authState.questionnaire = { role: "engineer", use_case: ["ship_code"] };
    workspaceState.workspaces = [];
    try {
      renderFlow({});

      expect(
        await screen.findByRole("img", { name: "Welcome to Multica" }),
      ).toBeInTheDocument();
      expect(screen.queryByText(/agent setup for/)).not.toBeInTheDocument();
    } finally {
      workspaceState.workspaces = [{ id: "ws-1", name: "Existing", slug: "existing" }];
    }
  });
});
