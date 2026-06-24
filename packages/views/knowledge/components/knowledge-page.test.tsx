import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import type { KnowledgeDetail, KnowledgeItem } from "@multica/core/knowledge/types";
import { KnowledgePage } from "./knowledge-page";

const mockReplace = vi.hoisted(() => vi.fn());
const mockUpdateKnowledge = vi.hoisted(() => vi.fn());
const mutationState = vi.hoisted(() => ({ updatePending: false }));
const queryState = vi.hoisted(() => ({
  detail: null as KnowledgeDetail | null,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/issues/${id}`,
    knowledgeDetail: (id: string) => `/knowledge/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: { children: React.ReactNode; href: string }) => (
    <a href={href} {...props}>{children}</a>
  ),
  useNavigation: () => ({
    replace: mockReplace,
    push: vi.fn(),
    pathname: "/knowledge/knowledge-1",
    searchParams: new URLSearchParams(),
  }),
}));

vi.mock("./knowledge-publish-dialogs", () => ({
  KnowledgePublishSkillDialog: () => null,
  KnowledgePublishWikiDialog: () => null,
}));

vi.mock("@multica/core/knowledge/queries", () => ({
  knowledgeListOptions: () => ({
    queryKey: ["knowledge", "ws-1", "list"],
    queryFn: () => Promise.resolve({ items: queryState.detail ? [queryState.detail.item] : [], total: queryState.detail ? 1 : 0 }),
  }),
  knowledgeDetailOptions: () => ({
    queryKey: ["knowledge", "ws-1", "detail", queryState.detail?.item.id ?? ""],
    queryFn: () => Promise.resolve(queryState.detail),
  }),
  knowledgeCandidatesOptions: () => ({
    queryKey: ["knowledge", "ws-1", "candidates"],
    queryFn: () => Promise.resolve({ candidates: [], total: 0 }),
  }),
  knowledgeGovernanceFindingsOptions: () => ({
    queryKey: ["knowledge", "ws-1", "governance-findings"],
    queryFn: () => Promise.resolve({ findings: [], total: 0 }),
  }),
  knowledgeAnalyticsOptions: () => ({
    queryKey: ["knowledge", "ws-1", "analytics"],
    queryFn: () => Promise.resolve({ items: [], total: 0 }),
  }),
  knowledgeEffectOptions: () => ({
    queryKey: ["knowledge", "ws-1", "effect"],
    queryFn: () => Promise.resolve({ items: [], total: 0 }),
  }),
  curatorDraftTaskOptions: () => ({
    queryKey: ["knowledge", "ws-1", "curator-draft", null],
    queryFn: () => Promise.resolve(null),
  }),
}));

vi.mock("@multica/core/knowledge/mutations", () => ({
  useUpdateKnowledge: () => ({
    isPending: mutationState.updatePending,
    mutateAsync: mockUpdateKnowledge,
  }),
  useReviewKnowledge: () => ({ isPending: false, mutateAsync: vi.fn() }),
  usePublishKnowledge: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useRegenerateKnowledgeEmbedding: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useDismissKnowledgeGovernance: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useArchiveKnowledge: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useRestoreKnowledge: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useCreateKnowledgeDraftFromCandidate: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useCreateKnowledgeDraftFromGovernanceFinding: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useResolveKnowledgeGovernanceFinding: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));

function makeKnowledgeItem(overrides: Partial<KnowledgeItem> = {}): KnowledgeItem {
  return {
    id: "knowledge-1",
    workspace_id: "ws-1",
    project_id: null,
    agent_id: null,
    title: "Original title",
    type: "lesson",
    domain_labels: ["frontend"],
    problem_pattern: "Original problem",
    trigger_conditions: "Original trigger",
    diagnostic_steps: "Original diagnostic steps",
    recommended_practice: "Original recommendation",
    anti_patterns: "Original anti-patterns",
    applicability: "Original applicability",
    confidence_status: "medium",
    lifecycle_status: "draft",
    created_by: "user-1",
    reviewed_by: null,
    reviewed_at: null,
    published_at: null,
    archived_at: null,
    updated_by: null,
    deprecated_at: null,
    stale_score: 0,
    effectiveness_score: 0,
    conflict_group: null,
    review_reason: null,
    update_suggestion: null,
    review_needed_at: null,
    governance_checked_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeKnowledgeDetail(item: KnowledgeItem = makeKnowledgeItem()): KnowledgeDetail {
  return {
    item,
    sources: [],
    source_summary: {
      count: 0,
      types: [],
      primary_source_type: "",
      primary_source_id: null,
      primary_source_title: "",
    },
    publish_targets: [],
    embeddings: [],
    embedding_status: null,
    feedback_summary: [],
  };
}

function renderKnowledgePage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <KnowledgePage knowledgeId="knowledge-1" />
    </QueryClientProvider>,
  );
}

async function saveButton() {
  return await screen.findByRole("button", { name: "Save" });
}

describe("KnowledgePage save button dirty state", () => {
  beforeEach(() => {
    mockReplace.mockReset();
    mockUpdateKnowledge.mockReset();
    mockUpdateKnowledge.mockImplementation(({ id, ...data }) =>
      Promise.resolve(makeKnowledgeItem({ id, ...data })),
    );
    mutationState.updatePending = false;
    queryState.detail = makeKnowledgeDetail();
  });

  it("keeps Save disabled until editable content changes", async () => {
    renderKnowledgePage();

    const save = await saveButton();
    expect(save).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("Knowledge title"), {
      target: { value: "Changed title" },
    });
    expect(save).toBeEnabled();

    fireEvent.change(screen.getByPlaceholderText("Knowledge title"), {
      target: { value: "Original title" },
    });
    expect(save).toBeDisabled();
  });

  it("enables Save when domain labels change", async () => {
    renderKnowledgePage();

    const save = await saveButton();
    expect(save).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("Labels, comma separated"), {
      target: { value: "frontend, backend" },
    });

    expect(save).toBeEnabled();
  });

  it("keeps Save disabled while update is pending", async () => {
    mutationState.updatePending = true;

    renderKnowledgePage();

    fireEvent.change(await screen.findByPlaceholderText("Knowledge title"), {
      target: { value: "Changed title" },
    });

    await waitFor(async () => {
      expect(await saveButton()).toBeDisabled();
    });
  });

  it("returns Save to disabled after a successful update", async () => {
    renderKnowledgePage();

    const save = await saveButton();
    fireEvent.change(screen.getByPlaceholderText("Knowledge title"), {
      target: { value: "Changed title" },
    });
    expect(save).toBeEnabled();

    fireEvent.click(save);

    await waitFor(() => {
      expect(mockUpdateKnowledge).toHaveBeenCalledWith(
        expect.objectContaining({ id: "knowledge-1", title: "Changed title" }),
      );
      expect(save).toBeDisabled();
    });
  });
});
