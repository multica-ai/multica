// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type {
  KnowledgeBase,
  KnowledgeBases,
  KnowledgeDocument,
  KnowledgeDocumentList,
  KnowledgeSearchResult,
  KnowledgeState,
  KnowledgeStatus,
} from "@multica/core/knowledge";
import { renderWithI18n } from "../test/i18n";
import { KnowledgePage } from "./knowledge-page";

vi.mock("@multica/core/api", () => ({
  api: {
    listKnowledgeBases: vi.fn(),
    getKnowledgeStatus: vi.fn(),
    enableKnowledge: vi.fn(),
    listKnowledgeDocuments: vi.fn(),
    searchKnowledge: vi.fn(),
    publishKnowledgeDocument: vi.fn(),
    discardKnowledgeDocument: vi.fn(),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
// The search's Ask the Secretary entry routes through the navigation adapter.
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));

let role: string | null = "admin";
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "user-1", role, member: null, isLoading: false }),
}));

const knowledgeBases: KnowledgeBase[] = [
  { id: "b1", key: "company", name: "Company", visibility: "workspace", position: 0 },
  { id: "b2", key: "market", name: "Market", visibility: "workspace", position: 1 },
];

function bases(state: KnowledgeState, offered = true): KnowledgeBases {
  return { offered, state, knowledge_bases: knowledgeBases };
}

function status(overrides: Partial<KnowledgeStatus> = {}): KnowledgeStatus {
  return {
    offered: true,
    reason: "",
    enabled: true,
    state: "ready",
    knowledge_bases: knowledgeBases,
    ui_url: "",
    ...overrides,
  };
}

function draft(overrides: Partial<KnowledgeDocument> = {}): KnowledgeDocument {
  return {
    id: "d1",
    document_id: "w1",
    knowledge_base: "market",
    title: "Acme profile",
    state: "draft",
    kind: "manual",
    url: "",
    source_type: "issue",
    source_id: "i1",
    created_by_type: "agent",
    created_by_id: "a1",
    created_at: "2026-11-01T00:00:00Z",
    published_at: null,
    error: "",
    ...overrides,
  };
}

let drafts: KnowledgeDocument[] = [];

function documentList(params?: { state?: string }): KnowledgeDocumentList {
  return {
    state: "ready",
    documents: params?.state === "draft" ? drafts : [],
  };
}

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <KnowledgePage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  role = "admin";
  drafts = [draft()];
  vi.mocked(toast.success).mockReset();
  vi.mocked(toast.error).mockReset();
  vi.mocked(api.listKnowledgeBases).mockReset().mockResolvedValue(bases("ready"));
  vi.mocked(api.getKnowledgeStatus).mockReset().mockResolvedValue(status());
  vi.mocked(api.enableKnowledge).mockReset().mockResolvedValue(status());
  vi.mocked(api.listKnowledgeDocuments)
    .mockReset()
    .mockImplementation(async (params) => documentList(params));
  vi.mocked(api.searchKnowledge).mockReset();
  vi.mocked(api.publishKnowledgeDocument).mockReset();
  vi.mocked(api.discardKnowledgeDocument).mockReset().mockResolvedValue(undefined);
});

describe("knowledge page states", () => {
  it("says knowledge is not offered when the deployment has no knowledge service", async () => {
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(bases("disabled", false));
    mount();
    expect(await screen.findByText("Knowledge is not offered on this server")).toBeVisible();
    expect(screen.queryByRole("tab", { name: "Library" })).toBeNull();
  });

  it("lets an admin enable knowledge for a workspace that has not set it up", async () => {
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(bases("disabled"));
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "Enable knowledge" }));
    await waitFor(() => expect(api.enableKnowledge).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Knowledge is enabled"));
  });

  it("tells a member to ask an owner or admin, with no enable action", async () => {
    role = "member";
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(bases("disabled"));
    mount();
    expect(await screen.findByText("Ask an owner or admin to enable it.")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Enable knowledge" })).toBeNull();
    expect(api.getKnowledgeStatus).not.toHaveBeenCalled();
  });

  it("explains the offline state and what agents do meanwhile", async () => {
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(bases("offline"));
    mount();
    expect(await screen.findByText("The knowledge service is offline")).toBeVisible();
    expect(
      screen.getByText("Agents keep working without company knowledge until it is back."),
    ).toBeVisible();
    expect(screen.queryByRole("tab", { name: "Library" })).toBeNull();
  });

  it("offers an admin a retry when setup failed", async () => {
    vi.mocked(api.listKnowledgeBases).mockResolvedValue(bases("failed"));
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "Retry setup" }));
    await waitFor(() => expect(api.enableKnowledge).toHaveBeenCalledTimes(1));
  });

  it("offers a retry when the page cannot load", async () => {
    vi.mocked(api.listKnowledgeBases).mockRejectedValueOnce(new Error("network down"));
    mount();
    expect(await screen.findByText("Could not load knowledge")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(api.listKnowledgeBases).toHaveBeenCalledTimes(2));
  });

  it("shows the library and drafts tabs when knowledge is ready", async () => {
    mount();
    expect(await screen.findByRole("tab", { name: "Library" })).toBeVisible();
    expect(screen.getByRole("tab", { name: "Drafts" })).toBeVisible();
  });
});

describe("knowledge page links and admin wiring", () => {
  it("links to the knowledge service UI for owners and admins", async () => {
    vi.mocked(api.getKnowledgeStatus).mockResolvedValue(status({ ui_url: "https://kb.example.test" }));
    mount();
    const link = await screen.findByRole("link", { name: "Open in WeKnora" });
    expect(link).toHaveAttribute("href", "https://kb.example.test");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("does not request the status or show the link to members", async () => {
    role = "member";
    mount();
    await screen.findByRole("tab", { name: "Library" });
    expect(api.getKnowledgeStatus).not.toHaveBeenCalled();
    expect(screen.queryByRole("link", { name: "Open in WeKnora" })).toBeNull();
  });
});

describe("knowledge search", () => {
  it("shows each hit's title, snippet, source and score", async () => {
    vi.mocked(api.searchKnowledge).mockResolvedValue({
      state: "ready",
      hits: [
        {
          document_id: "w1",
          knowledge_base: "market",
          title: "Acme profile",
          content: "Acme sells industrial sensors.",
          source: "https://acme.example.test/about",
          score: 0.8234,
        },
      ],
    } satisfies KnowledgeSearchResult);
    mount();
    fireEvent.change(await screen.findByRole("searchbox", { name: "Search company knowledge" }), {
      target: { value: "  pricing " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));

    expect(await screen.findByRole("heading", { name: "Acme profile" })).toBeVisible();
    expect(screen.getByText("Acme sells industrial sensors.")).toBeVisible();
    expect(screen.getByRole("link", { name: "https://acme.example.test/about" })).toHaveAttribute(
      "href",
      "https://acme.example.test/about",
    );
    expect(screen.getByText("Score 0.82")).toBeVisible();
    expect(api.searchKnowledge).toHaveBeenCalledWith({ q: "pricing" });
  });

  it("says when nothing matches", async () => {
    vi.mocked(api.searchKnowledge).mockResolvedValue({ state: "ready", hits: [] });
    mount();
    fireEvent.change(await screen.findByRole("searchbox", { name: "Search company knowledge" }), {
      target: { value: "unknown topic" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    expect(await screen.findByText("No matches")).toBeVisible();
  });
});

describe("knowledge documents", () => {
  it("filters by state and by knowledge base", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "Failed" }));
    await waitFor(() =>
      expect(api.listKnowledgeDocuments).toHaveBeenLastCalledWith({ kb: undefined, state: "failed" }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Market" }));
    await waitFor(() =>
      expect(api.listKnowledgeDocuments).toHaveBeenLastCalledWith({ kb: "market", state: "failed" }),
    );
  });
});

describe("knowledge drafts", () => {
  async function openDrafts() {
    mount();
    fireEvent.click(await screen.findByRole("tab", { name: "Drafts" }));
  }

  it("publishes a draft for an admin", async () => {
    vi.mocked(api.publishKnowledgeDocument).mockResolvedValue(draft({ state: "published" }));
    await openDrafts();
    fireEvent.click(await screen.findByRole("button", { name: "Publish" }));
    await waitFor(() => expect(api.publishKnowledgeDocument).toHaveBeenCalledWith("d1"));
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Draft published"));
  });

  it("discards a draft only after the confirmation, and closes once the server agrees", async () => {
    await openDrafts();
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("The draft is deleted and cannot be recovered.")).toBeVisible();
    expect(api.discardKnowledgeDocument).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Discard" }));
    await waitFor(() => expect(api.discardKnowledgeDocument).toHaveBeenCalledWith("d1"));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(toast.success).toHaveBeenCalledWith("Draft discarded");
  });

  it("keeps the dialog open and reports the failure when discard fails", async () => {
    vi.mocked(api.discardKnowledgeDocument).mockRejectedValue(new Error("offline"));
    await openDrafts();
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Discard" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Could not discard the draft"),
    );
    expect(screen.getByRole("alertdialog")).toBeVisible();
  });

  it("lists drafts to a member without publish or discard actions", async () => {
    role = "member";
    await openDrafts();
    expect(await screen.findByText("Acme profile")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Publish" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
  });
});
