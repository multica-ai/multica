import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@wallts/core/i18n/react";
import { configStore } from "@wallts/core/config";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { ConnectRemoteDialog } from "./connect-remote-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

vi.mock("@wallts/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@wallts/core/paths", () => ({
  paths: {
    workspace: () => ({
      agents: () => "/agents",
      runtimeDetail: () => "/runtimes/rt-test",
    }),
  },
  useWorkspaceSlug: () => "workspace-test",
}));

vi.mock("@wallts/core/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

function resetConfigStore() {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
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
  it("uses cloud setup commands by default", () => {
    const { baseElement } = renderDialog();

    expect(baseElement).toHaveTextContent("wallts setup");
    expect(baseElement).not.toHaveTextContent("wallts setup self-host");
    expect(baseElement).toHaveTextContent(
      "wallts config set server_url https://api.wallts.ai",
    );
    expect(baseElement).toHaveTextContent(
      "wallts config set app_url https://wallts.ai",
    );
  });

  it("uses self-host daemon URLs from runtime config", () => {
    const { baseElement } = renderDialog({
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    expect(baseElement).toHaveTextContent(
      "wallts setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "wallts config set server_url https://api.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "wallts config set app_url https://app.example.com",
    );
  });

  it("disables font ligatures in setup command code", () => {
    const { baseElement } = renderDialog();

    const setupCode = Array.from(baseElement.querySelectorAll("code")).find((node) =>
      node.textContent?.includes("wallts setup"),
    );

    expect(setupCode).toHaveClass(...ligatureClasses);
  });

  it("disables font ligatures in fallback token command code", () => {
    const { baseElement } = renderDialog();

    const tokenCode = Array.from(baseElement.querySelectorAll("code")).find((node) =>
      node.textContent?.includes("wallts login --token <YOUR_TOKEN>"),
    );

    expect(tokenCode).toHaveClass(...ligatureClasses);
  });
});
