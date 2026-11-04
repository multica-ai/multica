// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { KnowledgeBase, KnowledgeRepoSync } from "@multica/core/knowledge";
import { renderWithI18n } from "../test/i18n";
import { KnowledgeUploadPanel } from "./knowledge-upload";
import { KnowledgeSetupPanel } from "./knowledge-setup";

vi.mock("@multica/core/api", () => ({
  api: {
    uploadKnowledgeDocument: vi.fn(),
    createKnowledgeDocument: vi.fn(),
    listKnowledgeSources: vi.fn(),
    setKnowledgeVisibility: vi.fn(),
    updateKnowledgeBaseSettings: vi.fn(),
    getKnowledgeBaseAccess: vi.fn(),
    putKnowledgeGrant: vi.fn(),
    deleteKnowledgeGrant: vi.fn(),
    testKnowledgeModels: vi.fn(),
    revealKnowledgeOwnerPassword: vi.fn(),
    getKnowledgeRepoSync: vi.fn(),
    updateKnowledgeRepoSync: vi.fn(),
    runKnowledgeRepoSync: vi.fn(),
    listSquads: vi.fn().mockResolvedValue([]),
    listAgents: vi.fn().mockResolvedValue([]),
    listCompanyFloors: vi.fn().mockResolvedValue([]),
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const bases: KnowledgeBase[] = [
  { id: "b1", key: "market", name: "Market", visibility: "workspace", position: 0, wiki_enabled: false, summaries_enabled: true },
  { id: "b2", key: "company", name: "Company", visibility: "restricted", position: 1, wiki_enabled: true, summaries_enabled: true },
];

function mount(ui: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return renderWithI18n(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function fileInput(container: HTMLElement): HTMLInputElement {
  const input = container.querySelector('input[type="file"]');
  if (!(input instanceof HTMLInputElement)) throw new Error("no file input");
  return input;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.listSquads).mockResolvedValue([]);
  vi.mocked(api.listAgents).mockResolvedValue([]);
  vi.mocked(api.listCompanyFloors).mockResolvedValue([]);
});

describe("KnowledgeUploadPanel", () => {
  it("needs a file and a valid kind before it uploads", async () => {
    const { container } = mount(<KnowledgeUploadPanel wsId="ws-1" knowledgeBases={bases} />);
    const upload = screen.getByRole("button", { name: /upload/i });
    expect(upload).toBeDisabled();

    const file = new File(["# Pilot"], "pilot.md", { type: "text/markdown" });
    fireEvent.change(fileInput(container), { target: { files: [file] } });
    expect(upload).toBeDisabled(); // the kind is still missing

    fireEvent.change(screen.getByLabelText(/kind/i), { target: { value: "Market Report" } });
    expect(upload).toBeDisabled();
    expect(screen.getByText(/lowercase letters/i)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/kind/i), { target: { value: "market_report" } });
    expect(upload).toBeEnabled();
  });

  it("sends the base, the kind and the file", async () => {
    vi.mocked(api.uploadKnowledgeDocument).mockResolvedValue({
      id: "d1",
      document_id: "w1",
      knowledge_base: "market",
      title: "pilot.md",
      state: "published",
      kind: "manual",
      url: "",
      source_type: "upload",
      source_id: null,
      created_by_type: "member",
      created_by_id: "user-1",
      created_at: "",
      published_at: "",
      error: "",
    });
    const { container } = mount(<KnowledgeUploadPanel wsId="ws-1" knowledgeBases={bases} />);
    fireEvent.change(fileInput(container), {
      target: { files: [new File(["# Pilot"], "pilot.md", { type: "text/markdown" })] },
    });
    fireEvent.change(screen.getByLabelText(/kind/i), { target: { value: "market_report" } });
    fireEvent.click(screen.getByRole("button", { name: /upload/i }));

    await waitFor(() => expect(api.uploadKnowledgeDocument).toHaveBeenCalledTimes(1));
    const args = vi.mocked(api.uploadKnowledgeDocument).mock.calls[0]![0];
    expect(args).toMatchObject({ kb: "market", kind: "market_report" });
    expect(args.file.name).toBe("pilot.md");
  });

  it("imports a page with the URL as its title when none is given, published at once", async () => {
    vi.mocked(api.createKnowledgeDocument).mockResolvedValue({} as never);
    mount(<KnowledgeUploadPanel wsId="ws-1" knowledgeBases={bases} />);
    fireEvent.change(screen.getByLabelText(/page address/i), { target: { value: "https://example.com/a" } });
    fireEvent.click(screen.getByRole("button", { name: /import/i }));
    await waitFor(() => expect(api.createKnowledgeDocument).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.createKnowledgeDocument).mock.calls[0]![0]).toMatchObject({
      kb: "market",
      url: "https://example.com/a",
      title: "https://example.com/a",
      publish: true,
    });
  });
});

describe("KnowledgeSetupPanel", () => {
  it("shows a failed source with its error", async () => {
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({
      state: "ready",
      ui_url: "",
      sources: [
        {
          id: "ds1",
          knowledge_base: "company",
          name: "Handbook",
          type: "notion",
          status: "error",
          sync_schedule: "",
          last_sync_at: null,
          error_message: "token expired",
          total_items_synced: 0,
        },
      ],
    });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);
    expect(await screen.findByText("Handbook")).toBeInTheDocument();
    expect(screen.getByText("token expired")).toBeInTheDocument();
  });

  it("says the sources could not be loaded when the response is unreadable", async () => {
    // A malformed response falls back to the unknown state; it must not look like
    // an empty list.
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({ state: "unknown", ui_url: "", sources: [] });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);
    expect(await screen.findByText(/could not load the sources/i)).toBeInTheDocument();
  });

  it("lists the grants of a restricted base by name", async () => {
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({ state: "ready", ui_url: "", sources: [] });
    vi.mocked(api.listSquads).mockResolvedValue([{ id: "sq1", name: "Product Discovery" } as never]);
    vi.mocked(api.getKnowledgeBaseAccess).mockResolvedValue({
      key: "company",
      visibility: "restricted",
      grants: [{ subject_type: "squad", subject_id: "sq1", access: "write" }],
    });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);
    fireEvent.click(await screen.findByRole("button", { name: /access/i }));
    expect(await screen.findByText("Product Discovery")).toBeInTheDocument();
    expect(api.getKnowledgeBaseAccess).toHaveBeenCalledWith("company");
  });

  it("shows the owner's password once and nothing after", async () => {
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({ state: "ready", ui_url: "", sources: [] });
    vi.mocked(api.revealKnowledgeOwnerPassword).mockResolvedValue({
      email: "owner@example.test",
      password: "p4ss-once",
      ui_url: "http://ui.test",
    });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);
    fireEvent.click(screen.getByRole("button", { name: /show my password/i }));
    expect(await screen.findByText("p4ss-once")).toBeInTheDocument();
    expect(screen.getByText(/will not be shown again/i)).toBeInTheDocument();
  });

  it("names the model that failed and why", async () => {
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({ state: "ready", ui_url: "", sources: [] });
    vi.mocked(api.testKnowledgeModels).mockResolvedValue({
      configured: true,
      checks: [
        { role: "chat", name: "summary-chat", ok: true, error: "" },
        { role: "rerank", name: "rerank-model", ok: false, error: "upstream returned 404" },
      ],
    });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);
    fireEvent.click(screen.getByRole("button", { name: /test models/i }));
    expect(await screen.findByText("rerank-model")).toBeInTheDocument();
    expect(screen.getByText("upstream returned 404")).toBeInTheDocument();
  });
});

describe("KnowledgeSetupPanel repository docs sync", () => {
  const off: KnowledgeRepoSync = {
    enabled: false,
    repo_url: "",
    branch: "main",
    docs_path: "docs",
    knowledge_base_key: "engineering",
    last_synced_at: null,
    last_error: "",
  };

  beforeEach(() => {
    vi.mocked(api.listKnowledgeSources).mockResolvedValue({ state: "ready", ui_url: "", sources: [] });
  });

  it("saves the address, then syncs and shows the failed files of that run", async () => {
    // A stand-in server: each save and run changes what the next read returns.
    let server: KnowledgeRepoSync = { ...off };
    vi.mocked(api.getKnowledgeRepoSync).mockImplementation(async () => server);
    vi.mocked(api.updateKnowledgeRepoSync).mockImplementation(async (req) => {
      server = { ...server, ...req, last_synced_at: null, last_error: "" };
      return server;
    });
    vi.mocked(api.runKnowledgeRepoSync).mockImplementation(async () => {
      server = { ...server, last_synced_at: "2026-11-02T03:00:00Z", last_error: "docs/adr/0002.md: lacks the required headings" };
      return server;
    });
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);

    const address = await screen.findByLabelText(/repository address/i);
    fireEvent.change(address, { target: { value: "https://example.test/acme/app.git" } });
    fireEvent.click(screen.getByRole("switch", { name: /sync this repository every day/i }));
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() =>
      expect(api.updateKnowledgeRepoSync).toHaveBeenCalledWith({
        enabled: true,
        repo_url: "https://example.test/acme/app.git",
        branch: "main",
        docs_path: "docs",
        knowledge_base_key: "engineering",
      }),
    );

    fireEvent.click(await screen.findByRole("button", { name: /sync now/i }));
    expect(await screen.findByText("docs/adr/0002.md: lacks the required headings")).toBeInTheDocument();
    expect(api.runKnowledgeRepoSync).toHaveBeenCalledTimes(1);
  });

  it("does not offer a sync until a repository address is saved", async () => {
    vi.mocked(api.getKnowledgeRepoSync).mockResolvedValue(off);
    mount(<KnowledgeSetupPanel wsId="ws-1" knowledgeBases={bases} />);

    expect(await screen.findByRole("button", { name: /sync now/i })).toHaveAttribute("disabled");
    expect(screen.getByText(/never synced/i)).toBeInTheDocument();
  });
});
