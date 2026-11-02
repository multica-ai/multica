// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  createChatStore,
  DRAFT_NEW_SESSION,
  registerChatStore,
  useChatStore,
} from "@multica/core/chat";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { KnowledgeBase, KnowledgeHit } from "@multica/core/knowledge";
import { renderWithI18n } from "../test/i18n";
import { formatSecretaryResult } from "./ask-secretary";
import { KnowledgeSearch } from "./knowledge-search";

const memory = new Map<string, string>();
registerChatStore(
  createChatStore({
    storage: {
      getItem: (key) => memory.get(key) ?? null,
      setItem: (key, value) => void memory.set(key, value),
      removeItem: (key) => void memory.delete(key),
    },
  }),
);

vi.mock("@multica/core/api", () => ({ api: { searchKnowledge: vi.fn() } }));
const push = vi.fn();
vi.mock("../navigation", () => ({ useNavigation: () => ({ push }) }));

const knowledgeBases: KnowledgeBase[] = [
  { id: "b1", key: "company", name: "Company", visibility: "workspace", position: 0 },
  { id: "b2", key: "market", name: "Market", visibility: "workspace", position: 1 },
  { id: "b3", key: "engineering", name: "Engineering", visibility: "workspace", position: 3 },
];

const hits: KnowledgeHit[] = [
  {
    document_id: "d1",
    knowledge_base: "market",
    title: "Acme profile",
    content: "Acme sells anvils.",
    source: "https://acme.example/about",
    score: 0.91,
  },
  {
    document_id: "d2",
    knowledge_base: "engineering",
    title: "Deploy runbook",
    content: "Run the migration first.",
    source: "",
    score: 0.5,
  },
  { document_id: "d3", knowledge_base: "company", title: "Charter", content: "Ship what matters.", source: "", score: 0.3 },
  { document_id: "d4", knowledge_base: "company", title: "Fourth hit", content: "Not in the prompt.", source: "", score: 0.1 },
];

function mount({ secretaryAgentId, slug = "acme" }: { secretaryAgentId: string | null; slug?: string | null }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug={slug}>
        <KnowledgeSearch wsId="ws-1" knowledgeBases={knowledgeBases} secretaryAgentId={secretaryAgentId} />
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

async function search(query: string) {
  fireEvent.change(screen.getByRole("searchbox", { name: "Search company knowledge" }), {
    target: { value: query },
  });
  fireEvent.click(screen.getByRole("button", { name: "Search" }));
  await waitFor(() => expect(screen.getByText("Acme profile")).toBeInTheDocument());
}

describe("KnowledgeSearch: Ask the Secretary", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.searchKnowledge).mockResolvedValue({ state: "ready", hits });
    useChatStore.setState({
      inputDrafts: {},
      isOpen: false,
      floatingChatEnabled: true,
      activeSessionId: null,
      selectedAgentId: null,
    });
  });

  it("opens the Secretary's chat with the top results in an unsent prompt", async () => {
    mount({ secretaryAgentId: "agent-sec" });
    await search("pricing");

    fireEvent.click(screen.getByRole("button", { name: "Ask the Secretary" }));

    const chat = useChatStore.getState();
    expect(chat.selectedAgentId).toBe("agent-sec");
    expect(chat.activeSessionId).toBeNull();
    expect(chat.isOpen).toBe(true);
    expect(push).not.toHaveBeenCalled();

    const prompt = chat.inputDrafts[DRAFT_NEW_SESSION] ?? "";
    expect(prompt).toContain("Cite each document you use by its title and link");
    expect(prompt).toContain("Question: pricing");
    expect(prompt).toContain(
      "1. Acme profile (Market) - https://acme.example/about\nAcme sells anvils.",
    );
    expect(prompt).toContain("2. Deploy runbook (Engineering)\nRun the migration first.");
    expect(prompt).toContain("3. Charter (Company)");
    expect(prompt).not.toContain("Fourth hit");
  });

  it("opens the Chat tab on the Secretary when the floating chat is turned off", async () => {
    useChatStore.getState().setFloatingChatEnabled(false);
    mount({ secretaryAgentId: "agent-sec" });
    await search("pricing");

    fireEvent.click(screen.getByRole("button", { name: "Ask the Secretary" }));

    expect(push).toHaveBeenCalledWith("/acme/chat?agent=agent-sec");
    expect(useChatStore.getState().inputDrafts[DRAFT_NEW_SESSION]).toContain("Question: pricing");
  });

  it("is hidden when the workspace has no Secretary", async () => {
    mount({ secretaryAgentId: null });
    await search("pricing");

    expect(screen.queryByRole("button", { name: "Ask the Secretary" })).toBeNull();
  });

  it("is hidden when the search finds nothing to hand over", async () => {
    vi.mocked(api.searchKnowledge).mockResolvedValue({ state: "ready", hits: [] });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <KnowledgeSearch wsId="ws-1" knowledgeBases={knowledgeBases} secretaryAgentId="agent-sec" />
      </QueryClientProvider>,
    );
    fireEvent.change(screen.getByRole("searchbox", { name: "Search company knowledge" }), {
      target: { value: "nothing" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));

    await waitFor(() => expect(screen.getByText("No matches")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Ask the Secretary" })).toBeNull();
  });
});

describe("formatSecretaryResult", () => {
  it("keeps a short passage whole and cuts a long one with an ellipsis", () => {
    const long: KnowledgeHit = { ...hits[0]!, content: "a".repeat(700) };
    const text = formatSecretaryResult(long, 0, "Market");
    expect(text.startsWith("1. Acme profile (Market) - https://acme.example/about\n")).toBe(true);
    expect(text.endsWith("a".repeat(600) + "...")).toBe(true);
    expect(formatSecretaryResult(hits[2]!, 2, "Company")).toBe("3. Charter (Company)\nShip what matters.");
  });
});
