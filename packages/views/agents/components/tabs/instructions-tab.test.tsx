// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { configStore } from "@multica/core/config";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
// The conversation-starter editor previews the chat empty state, so it reads the
// chat namespace for the built-in defaults it renders when nothing is set.
import enChat from "../../../locales/en/chat.json";
import { NavigationProvider } from "../../../navigation";
import type { NavigationAdapter } from "../../../navigation";
import { InstructionsTab } from "./instructions-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, chat: enChat },
};
const persistedPrompt = {
  label: "Review a PR",
  prompt: "Review the open pull request.",
};
const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Reviewer",
  description: "",
  instructions: "Review carefully.",
  conversation_starters: [persistedPrompt],
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-08-24T00:00:00Z",
  updated_at: "2026-08-24T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function tab(agent: Agent, onSave = vi.fn().mockResolvedValue(undefined)) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <InstructionsTab agent={agent} onSave={onSave} />
    </I18nProvider>
  );
}

describe("InstructionsTab persisted-state synchronization", () => {
  beforeEach(() => {
    configStore.getState().setAgentConversationStartersSupported(true);
  });

  afterEach(() => {
    act(() => {
      configStore.getState().setAgentConversationStartersSupported(false);
    });
  });

  it("preserves an unsaved prompt across an equivalent agent-list refetch", async () => {
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent));
    const label = screen.getByLabelText("Suggestion 1 label");
    await user.clear(label);
    await user.type(label, "Inspect the patch");

    rerender(
      tab({
        ...baseAgent,
        conversation_starters: [{ ...persistedPrompt }],
      }),
    );

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
  });

  it("preserves dirty local state when persisted contents change", async () => {
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent));
    const label = screen.getByLabelText("Suggestion 1 label");
    await user.clear(label);
    await user.type(label, "Inspect the patch");

    rerender(
      tab({
        ...baseAgent,
        conversation_starters: [
          { label: "Server-side change", prompt: "A different prompt." },
        ],
      }),
    );

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
  });

  it("preserves submitted prompt edits when an optimistic update rolls back", async () => {
    let rejectSave!: (reason?: unknown) => void;
    const onSave = vi.fn(
      () =>
        new Promise<void>((_, reject) => {
          rejectSave = reject;
        }),
    );
    const user = userEvent.setup();
    const { rerender } = render(tab(baseAgent, onSave));
    const label = screen.getByLabelText("Suggestion 1 label");
    const prompt = screen.getByLabelText("Suggestion 1 prompt");
    await user.clear(label);
    await user.type(label, "Inspect the patch");
    await user.clear(prompt);
    await user.type(prompt, "Inspect the patch for correctness.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    const optimisticPrompt = {
      label: "Inspect the patch",
      prompt: "Inspect the patch for correctness.",
    };
    rerender(
      tab(
        {
          ...baseAgent,
          conversation_starters: [optimisticPrompt],
        },
        onSave,
      ),
    );
    rerender(
      tab(
        {
          ...baseAgent,
          conversation_starters: [{ ...persistedPrompt }],
        },
        onSave,
      ),
    );
    await act(async () => rejectSave(new Error("Update failed")));

    expect(screen.getByLabelText("Suggestion 1 label")).toHaveValue(
      "Inspect the patch",
    );
    expect(screen.getByLabelText("Suggestion 1 prompt")).toHaveValue(
      "Inspect the patch for correctness.",
    );
  });

  it("omits conversation starters from settings writes to an older backend", async () => {
    configStore.getState().setAgentConversationStartersSupported(false);
    const onSave = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(tab(baseAgent, onSave));

    expect(
      screen.queryByText("Conversation starters"),
    ).not.toBeInTheDocument();
    const instructions = screen.getByLabelText("System prompt");
    await user.clear(instructions);
    await user.type(instructions, "Updated instructions.");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).toHaveBeenCalledWith({
      instructions: "Updated instructions.",
    });
  });
});

// The "customize" link in a chat's empty state lands here with ?focus=. The
// tab must bring the editor into view and then clear the param, so a refresh
// or a later tab switch does not replay the flash.
describe("InstructionsTab conversation-starters deep link", () => {
  // Mirrors FOCUS_FLASH_MS in instructions-tab.tsx.
  const FLASH_MS = 1600;

  beforeEach(() => {
    configStore.getState().setAgentConversationStartersSupported(true);
  });

  afterEach(() => {
    act(() => {
      configStore.getState().setAgentConversationStartersSupported(false);
    });
  });

  // A fresh adapter object per render — what the web platform actually hands
  // down, and the condition the flash-timer regression below depends on.
  const adapter = (search: string, replace = vi.fn()): NavigationAdapter => ({
    push: vi.fn(),
    replace,
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(search),
    getShareableUrl: (path: string) => `https://app.test${path}`,
  });

  function renderWithSearch(search: string) {
    const replace = vi.fn();
    const scrollIntoView = vi.fn();
    // jsdom has no layout, so the method does not exist at all.
    Element.prototype.scrollIntoView = scrollIntoView;
    const tree = (value: NavigationAdapter, agent: Agent) => (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <NavigationProvider value={value}>
          <InstructionsTab agent={agent} onSave={vi.fn()} />
        </NavigationProvider>
      </I18nProvider>
    );
    const { rerender } = render(tree(adapter(search, replace), baseAgent));
    return {
      replace,
      scrollIntoView,
      /**
       * Re-render as the platform does: a new adapter object every time, and
       * optionally a different agent for an in-place navigation between two
       * agent pages on the same route.
       */
      settleUrl: (next: string, agent: Agent = baseAgent) =>
        rerender(tree(adapter(next, replace), agent)),
    };
  }

  const ringed = () =>
    document.querySelector<HTMLElement>("[class*='ring-brand']");

  it("scrolls to the editor and drops the focus param, keeping the view", () => {
    const { replace, scrollIntoView } = renderWithSearch(
      "view=instructions&focus=conversation_starters",
    );

    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    expect(replace).toHaveBeenCalledWith(
      "/acme/agents/agent-1?view=instructions",
    );
  });

  // Regression: the flash timer must not hang off the effect's cleanup. The
  // navigation adapter is not referentially stable, so React would run that
  // cleanup on the very next render and cancel the timeout, leaving the ring
  // on the editor permanently.
  it("ends the flash instead of ringing the editor forever", () => {
    vi.useFakeTimers();
    try {
      const { settleUrl } = renderWithSearch(
        "view=instructions&focus=conversation_starters",
      );
      expect(ringed()).not.toBeNull();

      // The stripped URL arrives on a new adapter object, re-running the
      // effect. A flash timer owned by that effect's cleanup dies here.
      act(() => settleUrl("view=instructions"));
      act(() => {
        vi.advanceTimersByTime(FLASH_MS + 400);
      });

      expect(ringed()).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  // Regression: the guard against re-firing before the stripped URL lands must
  // not become a one-shot-per-mount latch. A chat window left open beside this
  // page can send the very same link again, and it has to land again.
  it("focuses again when the link is clicked a second time", () => {
    const { replace, scrollIntoView, settleUrl } = renderWithSearch(
      "view=instructions&focus=conversation_starters",
    );
    expect(scrollIntoView).toHaveBeenCalledTimes(1);

    act(() => settleUrl("view=instructions"));
    act(() => settleUrl("view=instructions&focus=conversation_starters"));

    expect(scrollIntoView).toHaveBeenCalledTimes(2);
    expect(replace).toHaveBeenCalledTimes(2);
    expect(ringed()).not.toBeNull();
  });

  // Regression: the same guard is keyed by agent, so navigating between two
  // agent pages on this route does not swallow the second agent's link.
  it("focuses a link aimed at a different agent", () => {
    const other: Agent = { ...baseAgent, id: "agent-2" };
    const { scrollIntoView, settleUrl } = renderWithSearch(
      "view=instructions&focus=conversation_starters",
    );
    expect(scrollIntoView).toHaveBeenCalledTimes(1);

    // The URL still carries focus — it is the NEXT agent's deep link.
    act(() => settleUrl("view=instructions&focus=conversation_starters", other));

    expect(scrollIntoView).toHaveBeenCalledTimes(2);
  });

  it("ignores an ordinary visit", () => {
    const { replace, scrollIntoView } = renderWithSearch("view=instructions");

    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });
});
