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

// Mocked at the module boundary rather than through navigator.clipboard: jsdom
// exposes no clipboard, and user-event installs a getter-only stub of its own
// that would swallow the assertion. Assert what the button hands to copyText,
// not what the browser then does with it.
const clipboard = vi.hoisted(() => ({
  copyText: vi.fn<(text: string) => Promise<boolean>>(),
}));

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: clipboard.copyText }));

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

const WINDOWS_CMD =
  "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex";

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

describe("ConnectRemoteDialog", () => {
  beforeEach(() => {
    wsEventState.handler = null;
    clipboard.copyText.mockReset().mockResolvedValue(true);
  });

  it("uses cloud setup commands by default", () => {
    const { baseElement } = renderDialog();

    expect(baseElement).toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica setup self-host");
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://api.multica.ai",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://multica.ai",
    );
  });

  it("uses self-host daemon URLs from runtime config", () => {
    const { baseElement } = renderDialog({
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    expect(baseElement).toHaveTextContent(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://api.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://app.example.com",
    );
  });

  // The install command is OS-specific, so the dialog can't hardcode one.
  // Before this switch existed the dialog shipped only the curl command and
  // Windows users had no path at all, despite scripts/install.ps1. The switch
  // itself is covered in common/cli-install-command.test.tsx; this checks it
  // is wired through to step 1's copy button.
  it("copies the installer for the platform picked in step 1", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("tab", { name: "Windows" }));
    await user.click(screen.getAllByRole("button", { name: "Copy" })[0]!);

    await waitFor(() => {
      expect(clipboard.copyText).toHaveBeenCalledWith(WINDOWS_CMD);
    });
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
