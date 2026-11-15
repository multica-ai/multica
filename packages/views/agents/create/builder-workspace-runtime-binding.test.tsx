// @vitest-environment jsdom

import type { ReactNode } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chatKeys } from "@multica/core/chat/queries";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { runtimeModelsKeys } from "@multica/core/runtimes";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { AgentBuilderSessionSummary, RuntimeDevice } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { BuilderWorkspace } from "./builder-workspace";

const apiMocks = vi.hoisted(() => ({
  switchRuntime: vi.fn(),
  saveDraft: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: {
    switchAgentBuilderRuntime: apiMocks.switchRuntime,
    saveAgentBuilderDraft: apiMocks.saveDraft,
  },
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "member-1" } };
  return {
    useAuthStore: Object.assign(
      (selector: (value: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("./use-create-agent-submit", () => ({
  useCreateAgentSubmit: () => ({ creating: false, clearNameError: vi.fn() }),
}));
vi.mock("../../chat/components/use-chat-draft-restore", () => ({
  useChatDraftRestore: () => ({ restoreDraftRequest: null, handleRestoreDraftApplied: vi.fn() }),
}));
vi.mock("./builder-conversation", () => ({ BuilderConversation: () => null }));
vi.mock("./create-agent-footer", () => ({ CreateAgentFooter: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/avatar-upload-control", () => ({ AvatarUploadControl: () => null }));
vi.mock("../../runtimes/components/provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("../components/model-dropdown", () => ({ ModelDropdown: () => null }));
vi.mock("../components/skill-multi-select", () => ({ SkillMultiSelect: () => null }));
vi.mock("react-resizable-panels", () => ({ useDefaultLayout: () => ({}) }));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => <>{children}</>,
  ResizablePanel: ({ children }: { children: ReactNode }) => <>{children}</>,
  ResizableHandle: () => null,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const runtime: RuntimeDevice = {
  id: "runtime-1", workspace_id: "ws-1", daemon_id: null,
  name: "Claude (available.local)", custom_name: "Available machine",
  runtime_mode: "local", provider: "claude", launch_header: "", status: "online",
  device_info: "available.local", metadata: {}, owner_id: "member-1", visibility: "private",
  last_seen_at: null, created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z",
};
const session: AgentBuilderSessionSummary = {
  session_id: "builder-session", title: "Unbound draft", runtime_id: "",
  created_at: "", updated_at: "", last_message_content: "", last_message_role: "", last_message_at: "",
};

function mount(runtimes: RuntimeDevice[] = [runtime]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(runtimeKeys.list("ws-1"), runtimes);
  qc.setQueryData(workspaceKeys.members("ws-1"), []);
  qc.setQueryData(workspaceKeys.skills("ws-1"), []);
  qc.setQueryData(chatKeys.messages(session.session_id), []);
  qc.setQueryData(chatKeys.pendingTask(session.session_id), {});
  qc.setQueryData(runtimeModelsKeys.forRuntime(runtime.id), { models: [], supported: true, cached: false });
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <BuilderWorkspace sessionId={session.session_id} squadId={null}
        session={session} sessionSettled fallbackRuntimeId="stale-runtime-from-url"
        onDiscarded={vi.fn()} onRuntimeLabel={vi.fn()} />
    </QueryClientProvider>,
  );
  const trigger = () => {
    const button = view.container.querySelector<HTMLButtonElement>('[data-slot="popover-trigger"]');
    if (!button) throw new Error("runtime picker trigger is missing");
    return button;
  };
  return { ...view, trigger };
}

describe("unbound Builder runtime recovery", () => {
  beforeEach(() => {
    apiMocks.switchRuntime.mockReset();
    apiMocks.saveDraft.mockClear();
  });
  afterEach(cleanup);

  it("requires an explicit selection and a successful server rebind with one available runtime", async () => {
    let resolveBinding!: (result: { runtime_id: string }) => void;
    apiMocks.switchRuntime.mockImplementation(() => new Promise((resolve) => { resolveBinding = resolve; }));
    const view = mount();
    await waitFor(() => expect(view.trigger()).toBeEnabled());
    expect(view.trigger()).toHaveTextContent("Select a runtime first");
    expect(apiMocks.switchRuntime).not.toHaveBeenCalled();

    fireEvent.click(view.trigger());
    fireEvent.click(await screen.findByRole("button", { name: /Claude.*available\.local/ }));
    expect(apiMocks.switchRuntime).toHaveBeenCalledExactlyOnceWith(session.session_id, { runtime_id: runtime.id });
    expect(view.trigger()).toHaveTextContent("Select a runtime first");
    expect(view.trigger()).toBeDisabled();

    await act(async () => resolveBinding({ runtime_id: runtime.id }));
    await waitFor(() => expect(view.trigger()).toHaveTextContent("Available machine"));
    expect(view.trigger()).toBeEnabled();
  });

  it("keeps the empty picker state when there are no runtimes", () => {
    const view = mount([]);
    expect(view.trigger()).toHaveTextContent("No runtime available");
    expect(view.trigger()).toBeDisabled();
    expect(apiMocks.switchRuntime).not.toHaveBeenCalled();
  });
});
