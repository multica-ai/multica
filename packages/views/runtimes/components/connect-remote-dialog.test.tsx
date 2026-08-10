import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { ConnectRemoteDialog } from "./connect-remote-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: () => ({
      agents: () => "/agents",
      runtimeDetail: () => "/runtimes/rt-test",
    }),
  },
  useWorkspaceSlug: () => "workspace-test",
}));

const wsEventState = vi.hoisted(() => ({
  handler: null as ((payload: unknown) => void) | null,
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (_event: string, handler: (payload: unknown) => void) => {
    wsEventState.handler = handler;
  },
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

function resetConfigStore() {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
    googleClientId: "",
    daemonServerUrl: "",
    daemonAppUrl: "",
    workspaceCreationDisabled: false,
  });
}

function renderDialog(config?: {
  daemonServerUrl?: string;
  daemonAppUrl?: string;
}) {
  resetConfigStore();
  if (config) {
    configStore.getState().setDaemonConfig(config);
  }
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ConnectRemoteDialog onClose={vi.fn()} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const ligatureClasses = [
  "[font-variant-ligatures:none]",
  "[font-feature-settings:'liga'_0]",
];

describe("ConnectRemoteDialog", () => {
  beforeEach(() => {
    wsEventState.handler = null;
  });

  it("uses the fixed deployment without exposing a self-host setup choice", () => {
    const { baseElement } = renderDialog();

    expect(baseElement).toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica setup self-host");
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://multica.fluma.ai:26081",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://multica.fluma.ai:26081",
    );
  });

  it("does not turn runtime config into a self-host login command", () => {
    const { baseElement } = renderDialog({
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    expect(baseElement).toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica setup self-host");
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://multica.fluma.ai:26081",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://multica.fluma.ai:26081",
    );
  });

  it("switches the install command between macOS/Linux and Windows", async () => {
    const user = userEvent.setup();
    const { baseElement } = renderDialog();

    expect(screen.getByRole("button", { name: "macOS / Linux" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(baseElement).toHaveTextContent(
      "curl -fsSL https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.sh | bash",
    );

    await user.click(screen.getByRole("button", { name: "Windows" }));

    expect(screen.getByRole("button", { name: "Windows" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(baseElement).toHaveTextContent(
      "irm https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.ps1 | iex",
    );
    expect(baseElement).not.toHaveTextContent("curl -fsSL");
  });

  it("disables font ligatures in setup command code", () => {
    const { baseElement } = renderDialog();

    const setupCode = Array.from(baseElement.querySelectorAll("code")).find((node) =>
      node.textContent?.includes("multica setup"),
    );

    expect(setupCode).toHaveClass(...ligatureClasses);
  });

  it("disables font ligatures in fallback token command code", () => {
    const { baseElement } = renderDialog();

    const tokenCode = Array.from(baseElement.querySelectorAll("code")).find((node) =>
      node.textContent?.includes("multica login --token <YOUR_TOKEN>"),
    );

    expect(tokenCode).toHaveClass(...ligatureClasses);
  });

  it("transitions from setup instructions to the connected state", async () => {
    const { baseElement } = renderDialog();

    expect(baseElement).toHaveTextContent("multica setup");
    act(() => {
      wsEventState.handler?.({ runtime_id: "rt-test" });
    });

    await waitFor(() => {
      expect(screen.getByText("Computer connected")).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Create an agent" }),
      ).toBeInTheDocument();
    });
    expect(baseElement).not.toHaveTextContent("multica setup");
  });
});
