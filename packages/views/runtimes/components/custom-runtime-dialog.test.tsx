import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { CustomRuntimeDialog } from "./custom-runtime-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };
const mutationState = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
}));

vi.mock("@multica/core/runtimes/mutations", () => ({
  useAddCustomRuntime: () => ({
    mutateAsync: mutationState.mutateAsync,
    isPending: false,
  }),
  useUpdateCustomRuntime: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

const TARGET_RUNTIME: AgentRuntime = {
  id: "rt-claude",
  workspace_id: "ws-test",
  daemon_id: "daemon-test",
  name: "Claude (test-machine)",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "test-machine.local",
  metadata: {},
  owner_id: "user-test",
  visibility: "private",
  last_seen_at: "2026-06-02T00:00:00Z",
  created_at: "2026-06-02T00:00:00Z",
  updated_at: "2026-06-02T00:00:00Z",
};

function renderDialog({
  locale = "en",
  resources = TEST_RESOURCES,
  onClose = vi.fn(),
}: {
  locale?: SupportedLocale;
  resources?: Record<string, LocaleResources>;
  onClose?: () => void;
} = {}) {
  return render(
    <I18nProvider locale={locale} resources={resources}>
      <CustomRuntimeDialog
        wsId="ws-test"
        targetRuntime={TARGET_RUNTIME}
        onClose={onClose}
      />
    </I18nProvider>,
  );
}

function fillCustomRuntimeForm({
  provider,
  name,
  executable,
  args,
  resumeArgs,
  sessionIdRegex,
}: {
  provider: string;
  name: string;
  executable: string;
  args: string;
  resumeArgs?: string;
  sessionIdRegex?: string;
}) {
  fireEvent.change(screen.getByLabelText("Provider ID"), {
    target: { value: provider },
  });
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: name },
  });
  fireEvent.change(screen.getByLabelText("Executable"), {
    target: { value: executable },
  });
  fireEvent.change(screen.getByLabelText("Initial arguments"), {
    target: { value: args },
  });
  if (resumeArgs !== undefined) {
    fireEvent.change(screen.getByLabelText("Resume arguments"), {
      target: { value: resumeArgs },
    });
  }
  if (sessionIdRegex !== undefined) {
    fireEvent.change(screen.getByLabelText("Session ID regex"), {
      target: { value: sessionIdRegex },
    });
  }
}

describe("CustomRuntimeDialog", () => {
  beforeEach(() => {
    mutationState.mutateAsync.mockReset();
    mutationState.mutateAsync.mockResolvedValue({ status: "ok" });
  });

  it("shows a disabled primary action until the required CLI fields are filled", () => {
    renderDialog();

    expect(screen.getByRole("button", { name: "Add runtime" })).toBeDisabled();

    fillCustomRuntimeForm({
      provider: "codewhale",
      name: "CodeWhale",
      executable: "codewhale",
      args: "--model\nauto",
    });

    expect(screen.getByRole("button", { name: "Add runtime" })).toBeEnabled();
  });

  it("submits CodeWhale to the selected daemon runtime instead of generating an env command", async () => {
    const onClose = vi.fn();
    const { baseElement } = renderDialog({ onClose });
    fillCustomRuntimeForm({
      provider: "codewhale",
      name: "CodeWhale",
      executable: "codewhale",
      args: "exec\n--auto\n--output-format\nstream-json\n{{prompt}}",
      resumeArgs: "exec\n--resume\n{{session_id}}\n{{prompt}}",
      sessionIdRegex: String.raw`"sessionId":"([^"]+)"`,
    });

    fireEvent.click(screen.getByRole("button", { name: "Add runtime" }));

    await waitFor(() =>
      expect(mutationState.mutateAsync).toHaveBeenCalledWith({
        targetRuntimeId: "rt-claude",
        provider: "codewhale",
        name: "CodeWhale",
        path: "codewhale",
        args: ["exec", "--auto", "--output-format", "stream-json", "{{prompt}}"],
        resumeArgs: ["exec", "--resume", "{{session_id}}", "{{prompt}}"],
        sessionIdRegex: String.raw`"sessionId":"([^"]+)"`,
      }),
    );
    expect(onClose).toHaveBeenCalled();
    expect(baseElement).not.toHaveTextContent("MULTICA_CUSTOM_AGENTS");
  });

  it("starts without example defaults", () => {
    const { baseElement } = renderDialog();

    expect(screen.getByLabelText("Provider ID")).toHaveValue("");
    expect(screen.getByLabelText("Display name")).toHaveValue("");
    expect(screen.getByLabelText("Executable")).toHaveValue("");
    expect(screen.getByLabelText("Initial arguments")).toHaveValue("");
    expect(screen.getByLabelText("Resume arguments")).toHaveValue("");
    expect(screen.getByLabelText("Session ID regex")).toHaveValue("");
    expect(baseElement).not.toHaveTextContent("Code King");
    expect(baseElement).not.toHaveTextContent('"provider":"king"');
  });

  it("submits a traced Claude Code wrapper as a separate custom provider", async () => {
    renderDialog();

    fillCustomRuntimeForm({
      provider: "claude-traced",
      name: "Claude Code with tracing",
      executable: "claude",
      args: "-p\n--xxxId\nxxx\n{{prompt}}",
    });

    fireEvent.click(screen.getByRole("button", { name: "Add runtime" }));

    await waitFor(() =>
      expect(mutationState.mutateAsync).toHaveBeenCalledWith({
        targetRuntimeId: "rt-claude",
        provider: "claude-traced",
        name: "Claude Code with tracing",
        path: "claude",
        args: ["-p", "--xxxId", "xxx", "{{prompt}}"],
        resumeArgs: [],
        sessionIdRegex: "",
      }),
    );
  });
});
