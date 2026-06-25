// CEREBRO-PATCH(cerebro-unified-search): FIR-2022 — shared client-side RRF fusion for global search.
export type UnifiedSearchFilter = "all" | "issues" | "notes" | "projects" | "chats" | "messages";
export type UnifiedSearchType = Exclude<UnifiedSearchFilter, "all">;

export interface UnifiedSearchInputItem {
  id: string;
  title?: string | null;
  identifier?: string | null;
  updated_at?: string | null;
  created_at?: string | null;
}

export interface UnifiedSearchItem<T extends UnifiedSearchInputItem = UnifiedSearchInputItem> {
  id: string;
  type: UnifiedSearchType;
  item: T;
  score: number;
  boost: number;
}

export interface UnifiedSearchInput {
  issues?: UnifiedSearchInputItem[];
  notes?: UnifiedSearchInputItem[];
  projects?: UnifiedSearchInputItem[];
  chats?: UnifiedSearchInputItem[];
  messages?: UnifiedSearchInputItem[];
}

export interface UnifiedSearchResults {
  all: UnifiedSearchItem[];
  byFilter: Record<UnifiedSearchFilter, UnifiedSearchItem[]>;
  counts: Record<UnifiedSearchFilter, number>;
}

const RRF_K = 60;
const EMPTY_BY_FILTER: Record<UnifiedSearchFilter, UnifiedSearchItem[]> = {
  all: [],
  issues: [],
  notes: [],
  projects: [],
  chats: [],
  messages: [],
};

function norm(value: string | null | undefined) {
  return (value ?? "").trim().toLowerCase();
}

function timestamp(item: UnifiedSearchInputItem) {
  const raw = item.updated_at ?? item.created_at;
  return raw ? new Date(raw).getTime() || 0 : 0;
}

function boostFor(item: UnifiedSearchInputItem, query: string) {
  const q = norm(query);
  if (!q) return 0;
  if (norm(item.identifier) === q) return 2;
  if (norm(item.title) === q) return 1;
  return 0;
}

export function buildUnifiedSearchResults(input: UnifiedSearchInput, query: string): UnifiedSearchResults {
  const rows: UnifiedSearchItem[] = [];
  const addRows = (type: UnifiedSearchType, items: UnifiedSearchInputItem[] | undefined) => {
    (items ?? []).forEach((item, index) => {
      rows.push({
        id: item.id,
        type,
        item,
        score: 1 / (RRF_K + index + 1),
        boost: boostFor(item, query),
      });
    });
  };

  addRows("issues", input.issues);
  addRows("notes", input.notes);
  addRows("projects", input.projects);
  addRows("chats", input.chats);
  addRows("messages", input.messages);

  const all = rows.sort((a, b) => {
    if (b.boost !== a.boost) return b.boost - a.boost;
    if (b.score !== a.score) return b.score - a.score;
    return timestamp(b.item) - timestamp(a.item);
  });

  const byFilter: Record<UnifiedSearchFilter, UnifiedSearchItem[]> = {
    ...EMPTY_BY_FILTER,
    all,
    issues: all.filter((item) => item.type === "issues"),
    notes: all.filter((item) => item.type === "notes"),
    projects: all.filter((item) => item.type === "projects"),
    chats: all.filter((item) => item.type === "chats"),
    messages: all.filter((item) => item.type === "messages"),
  };

  return {
    all,
    byFilter,
    counts: {
      all: all.length,
      issues: byFilter.issues.length,
      notes: byFilter.notes.length,
      projects: byFilter.projects.length,
      chats: byFilter.chats.length,
      messages: byFilter.messages.length,
    },
  };
}
