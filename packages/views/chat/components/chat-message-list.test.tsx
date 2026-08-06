import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import type { TaskMessagePayload } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";

// The live timeline is a real list row rather than Virtuoso chrome (MUL-4922),
// so it shares one identity with the persisted assistant row and keeps its
// Mermaid/HTML blocks mounted across task completion. Real react-virtuoso
// renders its Footer but NO data rows under jsdom's zero-height viewport, so
// these tests must stub it to see rows at all. computeItemKey is still applied
// here — it is what expresses the live -> persisted identity.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
    components,
    context,
  }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    components?: { Footer?: (p: { context?: unknown }) => ReactElement | null };
    context?: unknown;
  }) => {
    const Footer = components?.Footer;
    return (
      <div>
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)} data-row-key={computeItemKey(i, item)}>
            {itemContent(i, item)}
          </div>
        ))}
        {Footer ? <Footer context={context} /> : null}
      </div>
    );
  },
}));

import { ChatMessageList } from "./chat-message-list";

const TEST_RESOURCES = { en: { chat: enChat } };
const TASK_ID = "6af44cbe-80ab-4dfe-b07d-bd3cfd588f4d";

function taskMsg(
  seq: number,
  type: TaskMessagePayload["type"],
  extra: Partial<TaskMessagePayload> = {},
): TaskMessagePayload {
  return { task_id: TASK_ID, seq, type, ...extra } as TaskMessagePayload;
}

// A streaming timeline whose middle (tool steps) is non-empty, so the live
// footer renders the "N steps" outer fold.
const INITIAL_MESSAGES: TaskMessagePayload[] = [
  taskMsg(0, "text", { content: "Looking into it. " }),
  taskMsg(1, "tool_use", { tool: "Bash", input: { command: "go test ./..." } }),
  taskMsg(2, "tool_result", { tool: "Bash", output: "ok" }),
];

function renderList(qc: QueryClient) {
  qc.setQueryData(chatKeys.taskMessages(TASK_ID), INITIAL_MESSAGES);
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatMessageList
          messages={[]}
          pendingTask={{ task_id: TASK_ID, status: "running" }}
          availability={undefined}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function pushTaskMessage(qc: QueryClient, msg: TaskMessagePayload) {
  // Mirrors useRealtimeSync's task:message handler: a new array lands in the
  // shared task-messages cache on every streamed message.
  act(() => {
    qc.setQueryData<TaskMessagePayload[]>(
      chatKeys.taskMessages(TASK_ID),
      (old = []) => [...old, msg],
    );
  });
}

describe("ChatMessageList live timeline (MUL-3960 regression)", () => {
  // The live footer is passed to Virtuoso through `components`. If that prop
  // is rebuilt inline on render, every streamed task:message unmounts and
  // remounts the whole footer subtree — re-parsing all Markdown and rebuilding
  // thousands of DOM rows, which froze the renderer during long agent runs.
  it("does not remount the live timeline when a streamed message arrives", async () => {
    const qc = new QueryClient();
    renderList(qc);

    const foldTrigger = await screen.findByText("2 steps");
    const footerBefore = foldTrigger.closest("div");

    pushTaskMessage(
      qc,
      taskMsg(3, "tool_use", { tool: "Read", input: { file_path: "/tmp/x" } }),
    );

    // The fold re-renders in place: same DOM node, updated count.
    const updatedTrigger = await screen.findByText("3 steps");
    expect(updatedTrigger.closest("div")).toBe(footerBefore);
    expect(document.contains(foldTrigger)).toBe(true);
  });

  it("keeps the process fold closed by the user across streamed messages", async () => {
    const qc = new QueryClient();
    renderList(qc);

    // Streaming defaults the fold open; the user closes it.
    const foldTrigger = await screen.findByText("2 steps");
    expect(screen.getByText("Bash")).toBeInTheDocument();
    act(() => {
      foldTrigger.click();
    });
    expect(screen.queryByText("Bash")).not.toBeInTheDocument();

    pushTaskMessage(
      qc,
      taskMsg(3, "tool_use", { tool: "Read", input: { file_path: "/tmp/x" } }),
    );

    // Before the fix the footer remounted, useState re-seeded defaultOpen and
    // the fold sprang back open on every streamed message.
    await screen.findByText("3 steps");
    expect(screen.queryByText("Bash")).not.toBeInTheDocument();
  });

  it("applies an embedded surface content transform to streamed text", async () => {
    const qc = new QueryClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [
      taskMsg(0, "text", {
        content:
          "Draft ready.\n<agent_draft>{\"name\":\"Hidden protocol\"}</agent_draft>",
      }),
    ]);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[]}
            pendingTask={{ task_id: TASK_ID, status: "running" }}
            availability="online"
            transformContent={(content) =>
              content.replace(
                /<agent_draft>[\s\S]*?<\/agent_draft>/g,
                "",
              )
            }
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByText("Draft ready.")).toBeInTheDocument();
    expect(screen.queryByText(/Hidden protocol/)).not.toBeInTheDocument();
  });

  it("hides a partial quick-actions protocol footer while text streams", async () => {
    const qc = new QueryClient();
    qc.setQueryData(chatKeys.taskMessages(TASK_ID), [
      taskMsg(0, "text", {
        content:
          "Draft ready.\n```quick-actions\n[{\"label\":\"Hidden suggestion\"",
      }),
    ]);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[]}
            pendingTask={{ task_id: TASK_ID, status: "running" }}
            availability="online"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByText("Draft ready.")).toBeInTheDocument();
    expect(screen.queryByText(/Hidden suggestion/)).not.toBeInTheDocument();
  });
});

describe("ChatMessageList quick actions", () => {
  it("renders up to three suggestions and sends the hidden prompt", async () => {
    const qc = new QueryClient();
    const onQuickAction = vi.fn();
    const quickActions = [
      { label: "Draft the brief", prompt: "Draft the complete launch brief", primary: true },
      { label: "Make a checklist", prompt: "Create a two-week launch checklist" },
      { label: "Define success", prompt: "Define the activation success metric" },
    ];

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[{
              id: "assistant-1",
              chat_session_id: "session-1",
              role: "assistant",
              content: "The plan is ready.",
              task_id: null,
              created_at: "2026-07-22T00:00:00Z",
              quick_actions: quickActions,
            }]}
            pendingTask={null}
            availability="online"
            onQuickAction={onQuickAction}
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByRole("button", { name: "Draft the brief" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Make a checklist" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Define success" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Draft the brief" }));
    expect(onQuickAction).toHaveBeenCalledWith(quickActions[0]);
  });

  it("disables suggestions while another reply is running", async () => {
    const qc = new QueryClient();
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[{
              id: "assistant-1",
              chat_session_id: "session-1",
              role: "assistant",
              content: "Ready.",
              task_id: null,
              created_at: "2026-07-22T00:00:00Z",
              quick_actions: [{ label: "Continue", prompt: "Continue" }],
            }]}
            pendingTask={null}
            availability="online"
            onQuickAction={vi.fn()}
            quickActionsDisabled
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByRole("button", { name: "Continue" })).toBeDisabled();
  });
});

describe("ChatMessageList quick actions skeleton", () => {
  const assistantMessage = {
    id: "assistant-1",
    chat_session_id: "session-1",
    role: "assistant" as const,
    content: "The plan is ready.",
    task_id: null,
    created_at: "2026-07-22T00:00:00Z",
  };

  it("renders pill skeletons for the message awaiting its supplement", () => {
    const qc = new QueryClient();
    const { container } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[assistantMessage]}
            pendingTask={null}
            availability="online"
            onQuickAction={vi.fn()}
            quickActionsPendingMessageId="assistant-1"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );
    expect(container.querySelectorAll(".rounded-full[aria-hidden] , [aria-hidden] .rounded-full").length).toBeGreaterThan(0);
    expect(screen.queryByRole("button", { name: /suggested/i })).toBeNull();
  });

  it("shows no skeleton for other messages or without onQuickAction", () => {
    const qc = new QueryClient();
    const { container } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatMessageList
            messages={[assistantMessage]}
            pendingTask={null}
            availability="online"
            quickActionsPendingMessageId="assistant-1"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );
    expect(container.querySelector("[aria-hidden] .rounded-full")).toBeNull();
  });
});

describe("ChatMessageList onboarding kickoff", () => {
  it("hides the product-authored kickoff while rendering Mika's reply", async () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={new QueryClient()}>
          <ChatMessageList
            messages={[
              {
                id: "kickoff",
                chat_session_id: "s1",
                role: "user",
                content: "INTERNAL ONBOARDING PROMPT",
                task_id: TASK_ID,
                created_at: new Date(0).toISOString(),
                message_kind: "onboarding_kickoff",
              },
              {
                id: "reply",
                chat_session_id: "s1",
                role: "assistant",
                content: "Hi, I'm Mika.",
                task_id: TASK_ID,
                created_at: new Date(1).toISOString(),
              },
            ]}
            pendingTask={undefined}
            availability="online"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(await screen.findByText("Hi, I'm Mika.")).toBeInTheDocument();
    expect(
      screen.queryByText("INTERNAL ONBOARDING PROMPT"),
    ).not.toBeInTheDocument();
  });
});

describe("ChatMessageList failure copy (MUL-5370 regression)", () => {
  // The backend moved to the refined taxonomy (agent_error.*) in MUL-2946 but
  // the copy map stayed on the six coarse values, so an exact-key lookup
  // missed every refined reason and fell through to the generic fallback.
  // A user whose skill bundle download stalled was told only "Something went
  // wrong", and the maintainer debugging it had nothing better to go on.
  function renderFailure(reason: string) {
    return render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={new QueryClient()}>
          <ChatMessageList
            messages={[
              {
                id: "m1",
                chat_session_id: "s1",
                role: "assistant",
                content: "skill bundle unavailable: skill \"x\"",
                task_id: null,
                created_at: new Date(0).toISOString(),
                failure_reason: reason,
              },
            ]}
            pendingTask={undefined}
            availability="online"
          />
        </QueryClientProvider>
      </I18nProvider>,
    );
  }

  const FALLBACK = enChat.message_list.failure.fallback;

  it("renders dedicated copy for a stalled skill bundle download", async () => {
    renderFailure("skill_bundle_unavailable");
    expect(
      await screen.findByText(enChat.message_list.failure.skill_bundle_unavailable),
    ).toBeInTheDocument();
    expect(screen.queryByText(FALLBACK)).not.toBeInTheDocument();
  });

  it("renders dedicated copy for a refined reason the map names", async () => {
    renderFailure("agent_error.provider_network");
    expect(
      await screen.findByText(enChat.message_list.failure.provider_network),
    ).toBeInTheDocument();
  });

  it("degrades an unnamed refined reason to its agent_error family", async () => {
    renderFailure("agent_error.unknown");
    expect(
      await screen.findByText(enChat.message_list.failure.agent_error),
    ).toBeInTheDocument();
    expect(screen.queryByText(FALLBACK)).not.toBeInTheDocument();
  });

  it("degrades a reason newer than this build to its agent_error family", async () => {
    renderFailure("agent_error.some_future_bucket");
    expect(
      await screen.findByText(enChat.message_list.failure.agent_error),
    ).toBeInTheDocument();
  });

  it("still falls back when neither the reason nor its family is known", async () => {
    renderFailure("something_entirely_new");
    expect(await screen.findByText(FALLBACK)).toBeInTheDocument();
  });
});
