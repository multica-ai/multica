// @vitest-environment jsdom

import { useState, type ReactNode } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EMPTY_AGENT_DRAFT, toStoredAgentDraft, type AgentDraft } from "@multica/core/agents";
import type { AgentBuilderSessionSummary, ChatMessage } from "@multica/core/types";
import type { SupportedLocale } from "@multica/core/i18n";
import { renderWithI18n } from "../../test/i18n";
import { BuilderWorkspace } from "./builder-workspace";

const h = vi.hoisted(() => ({
  messages: [] as ChatMessage[],
  pending: false,
  loading: false,
  settled: true,
  error: null as string | null,
  session: undefined as AgentBuilderSessionSummary | undefined,
  save: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@multica/core/api", () => ({
  api: { saveAgentBuilderDraft: h.save },
}));
vi.mock("./use-builder-session", () => ({
  useBuilderSession: () => ({
    messages: h.messages,
    pending: h.pending,
    messagesLoading: h.loading,
    error: h.error,
  }),
}));
vi.mock("./use-create-agent-form", () => ({
  useCreateAgentForm: () => {
    const [draft, setDraft] = useState({ ...EMPTY_AGENT_DRAFT, runtimeId: "local" });
    return {
      draft, setDraft, selectedRuntime: null,
      workspaceSkills: [], members: [], runtimes: [], draftReady: true,
    };
  },
}));
vi.mock("./use-create-agent-submit", () => ({
  useCreateAgentSubmit: () => ({ creating: false }),
}));
vi.mock("./agent-configuration-panel", () => ({
  AgentConfigurationPanel: ({ draft, onChange }: {
    draft: AgentDraft; onChange: (draft: AgentDraft) => void;
  }) => (
    <input aria-label="Agent name" value={draft.name}
      onChange={(event) => onChange({ ...draft, name: event.target.value })} />
  ),
}));
vi.mock("./builder-conversation", () => ({
  BuilderConversation: ({ error }: { error: string | null }) =>
    error ? <div role="alert">{error}</div> : null,
}));
vi.mock("./create-agent-footer", () => ({ CreateAgentFooter: () => null }));
vi.mock("react-resizable-panels", () => ({ useDefaultLayout: () => ({}) }));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => <>{children}</>,
  ResizablePanel: ({ children }: { children: ReactNode }) => <>{children}</>,
  ResizableHandle: () => null,
}));

const guidance = "This reply could not fill the form. Ask the assistant to generate the draft again, or fill it in manually.";
const unclosed = '<agent_draft>{"name":"Poet","instructions":"# Role\nWrite poetry."}';
function reply(content: string, id = "reply-1"): ChatMessage {
  return {
    id, content, role: "assistant", chat_session_id: "session-1",
    task_id: "task-1", created_at: "2026-09-17T05:00:00Z",
  };
}
function workspace() {
  return <BuilderWorkspace sessionId="session-1" squadId={null}
    session={h.session} sessionSettled={h.settled} fallbackRuntimeId="local"
    onDiscarded={vi.fn()} onRuntimeLabel={vi.fn()} />;
}
function mount(locale: SupportedLocale = "en") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>{workspace()}</QueryClientProvider>,
    { locale },
  );
  return {
    ...view,
    refresh: () => view.rerender(
      <QueryClientProvider client={qc}>{workspace()}</QueryClientProvider>,
    ),
  };
}

describe("builder draft recovery wiring", () => {
  beforeEach(() => {
    h.messages = [];
    h.pending = false;
    h.loading = false;
    h.settled = true;
    h.error = null;
    h.session = undefined;
    h.save.mockClear();
  });
  afterEach(cleanup);

  it.each([unclosed, `${unclosed}</agent_draft>`])("waits for completion, fills once, and persists subsequent manual edits: %s", async (content) => {
    h.messages = [
      reply('<agent_draft>{"name":"Older draft"}</agent_draft>', "older"),
      reply(content),
    ];
    h.pending = true;
    const view = mount();
    expect(screen.getByLabelText("Agent name")).toHaveValue("");
    expect(screen.queryByRole("alert")).toBeNull();

    h.pending = false;
    view.refresh();
    await waitFor(() => expect(screen.getByLabelText("Agent name")).toHaveValue("Poet"));
    fireEvent.change(screen.getByLabelText("Agent name"), { target: { value: "Edited poet" } });
    h.messages = [...h.messages];
    view.refresh();
    expect(screen.getByLabelText("Agent name")).toHaveValue("Edited poet");
    expect(screen.queryByRole("alert")).toBeNull();
    h.pending = true;
    view.refresh();
    expect(screen.getByLabelText("Agent name")).toHaveValue("Edited poet");
    h.pending = false;
    view.refresh();
    expect(screen.getByLabelText("Agent name")).toHaveValue("Edited poet");
    view.unmount();
    expect(h.save).toHaveBeenLastCalledWith("session-1", expect.objectContaining({
      name: "Edited poet", applied_message_id: "reply-1",
    }));
  });

  it("shows recovery guidance for an invalid final reply and clears it on a valid retry", async () => {
    h.messages = [reply('<agent_draft>{"name":"Unfinished')];
    h.pending = true;
    const view = mount();
    expect(screen.queryByRole("alert")).toBeNull();
    h.pending = false;
    view.refresh();
    expect(screen.getByRole("alert")).toHaveTextContent(guidance);
    expect(screen.getByLabelText("Agent name")).toHaveValue("");

    h.messages = [...h.messages, reply(unclosed, "reply-2")];
    view.refresh();
    await waitFor(() => expect(screen.getByLabelText("Agent name")).toHaveValue("Poet"));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each([unclosed, `${unclosed}</agent_draft>`])("does not recover a failed turn or replace its existing failure presentation: %s", (content) => {
    h.messages = [{ ...reply(content), failure_reason: "agent_error" }];
    mount();
    expect(screen.getByLabelText("Agent name")).toHaveValue("");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("preserves saved manual edits when reopening a recovered reply", async () => {
    h.messages = [reply(unclosed)];
    h.session = {
      session_id: "session-1", title: "Poet", runtime_id: "local",
      created_at: "", updated_at: "", last_message_at: "",
      last_message_content: unclosed, last_message_role: "assistant",
      draft: toStoredAgentDraft({ ...EMPTY_AGENT_DRAFT, name: "Saved edit" }, "reply-1"),
    };
    mount();
    await waitFor(() => expect(screen.getByLabelText("Agent name")).toHaveValue("Saved edit"));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(h.save).not.toHaveBeenCalled();
  });

  it("waits for history and stored configuration before recovering a draft", async () => {
    h.messages = [reply(unclosed)];
    h.loading = true;
    h.settled = false;
    const view = mount();
    expect(screen.getByLabelText("Agent name")).toHaveValue("");
    h.loading = false;
    view.refresh();
    expect(screen.getByLabelText("Agent name")).toHaveValue("");
    h.settled = true;
    view.refresh();
    await waitFor(() => expect(screen.getByLabelText("Agent name")).toHaveValue("Poet"));
  });

  it("keeps transport errors visible ahead of draft guidance", () => {
    h.messages = [reply('<agent_draft>{"name":"Unfinished')];
    h.error = "Connection lost";
    mount();
    expect(screen.getByRole("alert")).toHaveTextContent("Connection lost");
    expect(screen.getByRole("alert")).not.toHaveTextContent(guidance);
  });

  it.each(["Stopped.", "No draft"])("does not warn for a reply without a draft block, including after reopening: %s", (content) => {
    h.messages = [reply(content)];
    const view = mount();
    expect(screen.queryByRole("alert")).toBeNull();
    view.unmount();
    mount();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("does not warn for empty history, user messages, or no-response replies", () => {
    const view = mount();
    expect(screen.queryByRole("alert")).toBeNull();
    h.messages = [{ ...reply("Create a poet"), role: "user" }];
    view.refresh();
    expect(screen.queryByRole("alert")).toBeNull();
    h.messages = [{ ...reply(""), message_kind: "no_response" }];
    view.refresh();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each([
    ["en", "This reply could not fill the form."],
    ["zh-Hans", "本次回复未能填入表单。"],
    ["ja", "この返信からフォームを入力できませんでした。"],
    ["ko", "이 응답으로 양식을 채울 수 없습니다."],
    ["fr", "Cette réponse n’a pas permis de remplir le formulaire."],
  ] as const)("renders draft recovery guidance in %s", (locale, message) => {
    h.messages = [reply('<agent_draft>{"name":"Unfinished')];
    mount(locale);
    expect(screen.getByRole("alert")).toHaveTextContent(message);
  });
});
