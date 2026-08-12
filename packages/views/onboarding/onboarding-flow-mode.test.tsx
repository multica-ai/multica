import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) =>
      selector({ user: { id: "u-1", onboarding_questionnaire: {} } }),
    { getState: () => ({ user: { id: "u-1" } }) },
  ),
}));

// The flow only queries the workspace list; returning one proves the
// new-workspace mode does not offer to continue with it.
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: () => ({
      data: [{ id: "ws-1", name: "Existing", slug: "existing" }],
      isFetched: true,
    }),
  };
});

vi.mock("@multica/core/onboarding", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@multica/core/onboarding",
  );
  return { ...actual, useBootstrapMika: () => ({ mutateAsync: vi.fn() }) };
});

import { OnboardingFlow } from "./onboarding-flow";

function renderFlow(props: Record<string, unknown>) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <OnboardingFlow onComplete={vi.fn()} {...props} />
    </I18nProvider>,
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
    renderFlow({});

    expect(
      screen.queryByRole("heading", { name: /Name your workspace/i }),
    ).not.toBeInTheDocument();
  });
});
