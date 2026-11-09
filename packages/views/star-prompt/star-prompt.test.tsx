import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance, type ApiClient } from "@multica/core/api";
import { configStore } from "@multica/core/config";
import { I18nProvider } from "@multica/core/i18n/react";
import {
  claimStarPromptShowing,
  markStarPromptStarred,
  useStarPromptStore,
} from "@multica/core/star-prompt";
import type { User } from "@multica/core/types";
import enLayout from "../locales/en/layout.json";
import { STAR_PROMPT_DELAY_MS, StarPrompt } from "./star-prompt";

const stores = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { create } = require("zustand") as typeof import("zustand");
  return {
    auth: create<{ user: Partial<User> | null }>(() => ({ user: null })),
    chat: create<{ isOpen: boolean; floatingChatEnabled: boolean }>(() => ({
      isOpen: false,
      floatingChatEnabled: true,
    })),
  };
});

vi.mock("@multica/core/auth", () => ({ useAuthStore: stores.auth }));
vi.mock("@multica/core/chat", () => ({ useChatStore: stores.chat }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ chat: () => "/acme/chat" }) }));
vi.mock("../navigation", () => ({ useNavigation: () => ({ pathname: "/acme/issues" }) }));
vi.mock("@multica/core/star-prompt", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/star-prompt")>(
    "@multica/core/star-prompt",
  );
  return {
    ...actual,
    claimStarPromptShowing: vi.fn(),
    markStarPromptStarred: vi.fn(() => Promise.resolve()),
  };
});

const DAY_MS = 24 * 60 * 60 * 1000;
const listIssues = vi.fn();
let qc: QueryClient;

function eligibleUser(): Partial<User> {
  return {
    id: "u1",
    created_at: new Date(Date.now() - 10 * DAY_MS).toISOString(),
    // Already answered the source survey, so it does not claim the session.
    onboarded_at: "2026-01-01T00:00:00Z",
    onboarding_questionnaire: { source: ["friend"] },
    star_prompt_shown_count: 0,
    star_prompt_last_shown_at: null,
    star_prompt_starred_at: null,
  };
}

function renderPrompt() {
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { layout: enLayout } }}>
        <StarPrompt />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function agentFinishesIssue() {
  act(() => {
    useStarPromptStore.getState().reportAgentIssueDone({ wsId: "ws-1", agentId: "agent-1", issueId: "i1" });
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(STAR_PROMPT_DELAY_MS);
  });
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  listIssues.mockResolvedValue({ issues: [], total: 4 });
  setApiInstance({
    listIssues,
    listAgents: vi.fn().mockResolvedValue([{ id: "agent-1", name: "Lambda" }]),
  } as unknown as ApiClient);
  vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new Error("offline"))));
  vi.mocked(claimStarPromptShowing).mockResolvedValue(true);
  stores.auth.setState({ user: eligibleUser() });
  stores.chat.setState({ isOpen: false });
  useStarPromptStore.setState({ moment: null, shownThisSession: false });
});

afterEach(() => {
  qc.clear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  configStore.getState().setStarPromptDisabled(false);
});

describe("StarPrompt", () => {
  it("celebrates the agent a moment after it finishes enough issues", async () => {
    renderPrompt();
    await agentFinishesIssue();

    expect(await screen.findByText("Lambda has completed 4 issues")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Star Multica on GitHub" })).toHaveAttribute(
      "href",
      "https://github.com/multica-ai/multica",
    );
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ assignee_filters: [{ type: "agent", id: "agent-1" }] }),
    );
  });

  it("retires the prompt when the user clicks through to GitHub", async () => {
    renderPrompt();
    await agentFinishesIssue();
    fireEvent.click(await screen.findByRole("link", { name: "Star Multica on GitHub" }));

    expect(markStarPromptStarred).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Lambda has completed 4 issues")).not.toBeInTheDocument();
  });

  it("closes on Not now without retiring the prompt", async () => {
    renderPrompt();
    await agentFinishesIssue();
    fireEvent.click(await screen.findByRole("button", { name: "Not now" }));

    expect(screen.queryByText("Lambda has completed 4 issues")).not.toBeInTheDocument();
    expect(markStarPromptStarred).not.toHaveBeenCalled();
  });

  it("stays hidden until the agent has finished three issues", async () => {
    listIssues.mockResolvedValue({ issues: [], total: 2 });
    renderPrompt();
    await agentFinishesIssue();

    expect(claimStarPromptShowing).not.toHaveBeenCalled();
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });

  it("stays hidden when the server refuses the showing", async () => {
    vi.mocked(claimStarPromptShowing).mockResolvedValue(false);
    renderPrompt();
    await agentFinishesIssue();

    expect(claimStarPromptShowing).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Lambda has completed 4 issues")).not.toBeInTheDocument();
  });

  it("asks at most once per session", async () => {
    renderPrompt();
    await agentFinishesIssue();
    fireEvent.click(await screen.findByRole("button", { name: "Not now" }));
    await agentFinishesIssue();

    expect(claimStarPromptShowing).toHaveBeenCalledTimes(1);
  });

  it("yields to the source survey when it is still owed", async () => {
    stores.auth.setState({ user: { ...eligibleUser(), onboarding_questionnaire: {} } });
    renderPrompt();
    await agentFinishesIssue();

    expect(listIssues).not.toHaveBeenCalled();
  });

  it("skips the moment while the user is typing", async () => {
    renderPrompt();
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    await agentFinishesIssue();
    input.remove();

    expect(listIssues).not.toHaveBeenCalled();
  });

  it("does nothing when the deployment turns it off", async () => {
    configStore.getState().setStarPromptDisabled(true);
    renderPrompt();
    await agentFinishesIssue();

    expect(listIssues).not.toHaveBeenCalled();
  });
});
