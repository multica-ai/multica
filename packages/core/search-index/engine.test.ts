// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { SearchIndexComment, SearchIndexIssue, SearchIndexProject } from "../types";
import {
  SearchIndexEngine,
  utf8Length,
  extractSnippet,
  splitSearchTerms,
  timestampMicros,
  toSearchIssueResult,
  toSearchProjectResult,
} from "./engine";

let seq = 0;

function issue(overrides: Partial<SearchIndexIssue> & { title: string }): SearchIndexIssue {
  seq += 1;
  const id = overrides.id ?? `00000000-0000-0000-0000-${String(seq).padStart(12, "0")}`;
  return {
    id,
    workspace_id: "ws",
    number: seq,
    identifier: `MUL-${overrides.number ?? seq}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    search_updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as SearchIndexIssue;
}

function comment(issueId: string, content: string, createdAt: string, id?: string): SearchIndexComment {
  seq += 1;
  return {
    id: id ?? `c0000000-0000-0000-0000-${String(seq).padStart(12, "0")}`,
    issue_id: issueId,
    content,
    created_at: createdAt,
  };
}

function project(overrides: Partial<SearchIndexProject> & { title: string }): SearchIndexProject {
  seq += 1;
  return {
    id: `p0000000-0000-0000-0000-${String(seq).padStart(12, "0")}`,
    workspace_id: "ws",
    description: null,
    icon: null,
    status: "planned",
    priority: "none",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    search_updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as SearchIndexProject;
}

function engineWith(issues: SearchIndexIssue[], comments: SearchIndexComment[] = []) {
  const engine = new SearchIndexEngine();
  issues.forEach((i) => engine.upsertIssue(i));
  comments.forEach((c) => engine.upsertComment(c));
  return engine;
}

describe("SearchIndexEngine.searchIssues ranking", () => {
  it("orders the relevance tiers the way server search does", () => {
    const at = (minute: number) => `2026-01-01T00:${String(minute).padStart(2, "0")}:00Z`;
    const commentAll = issue({ title: "unrelated five", search_updated_at: at(9) });
    const commentPhrase = issue({ title: "unrelated four", search_updated_at: at(8) });
    const descAll = issue({ title: "unrelated three", description: "gamma then alpha", search_updated_at: at(7) });
    const descPhrase = issue({ title: "unrelated two", description: "has alpha gamma inside", search_updated_at: at(6) });
    const titleAll = issue({ title: "gamma before alpha", search_updated_at: at(5) });
    const titleContains = issue({ title: "the alpha gamma here", search_updated_at: at(4) });
    const titleStarts = issue({ title: "alpha gamma first", search_updated_at: at(3) });
    const titleExact = issue({ title: "Alpha Gamma", search_updated_at: at(2) });
    const engine = engineWith(
      [commentAll, commentPhrase, descAll, descPhrase, titleAll, titleContains, titleStarts, titleExact],
      [
        comment(commentPhrase.id, "comment says alpha gamma", at(1)),
        comment(commentAll.id, "gamma and later alpha", at(1)),
      ],
    );

    const hits = engine.searchIssues({ q: "alpha gamma", include_closed: true });
    expect(hits.map((h) => h.id)).toEqual([
      titleExact.id,
      titleStarts.id,
      titleContains.id,
      titleAll.id,
      descPhrase.id,
      descAll.id,
      commentPhrase.id,
      commentAll.id,
    ]);
    expect(hits.map((h) => h.matchSource)).toEqual([
      "title",
      "title",
      "title",
      "title",
      "description",
      "description",
      "comment",
      "comment",
    ]);
  });

  it("breaks ties by status, then precise updated_at, then id", () => {
    const backlog = issue({ title: "tie token", status: "backlog", search_updated_at: "2026-01-01T00:00:00.000009Z" });
    const inProgress = issue({ title: "tie token", status: "in_progress", search_updated_at: "2026-01-01T00:00:00Z" });
    const older = issue({ title: "tie token", status: "todo", search_updated_at: "2026-01-01T00:00:00.000001Z" });
    const newer = issue({ title: "tie token", status: "todo", search_updated_at: "2026-01-01T00:00:00.000002Z" });
    const sameA = issue({ id: "a0000000-0000-0000-0000-000000000000", title: "tie token", status: "blocked" });
    const sameB = issue({ id: "b0000000-0000-0000-0000-000000000000", title: "tie token", status: "blocked" });
    const engine = engineWith([backlog, sameB, older, inProgress, newer, sameA]);

    expect(engine.searchIssues({ q: "tie token" }).map((h) => h.id)).toEqual([
      inProgress.id,
      newer.id,
      older.id,
      sameA.id,
      sameB.id,
      backlog.id,
    ]);
  });

  it.each(["MUL", "V2", "A1"])("puts a %s identifier or bare number first, even without a text match", (prefix) => {
    const target = issue({ title: "no text overlap", number: 4242, identifier: `${prefix}-4242` });
    const textHit = issue({ title: "mentions 4242 in the title" });
    const engine = engineWith([textHit, target]);

    const byIdentifier = engine.searchIssues({ q: `  ${prefix.toLowerCase()}-4242  ` });
    expect(byIdentifier.map((h) => h.id)).toEqual([target.id]);
    expect(byIdentifier[0]!.matchSource).toBe("comment");

    expect(engine.searchIssues({ q: "4242" }).map((h) => h.id)).toEqual([target.id, textHit.id]);
  });

  it("keeps a cancelled digit-prefix identifier ahead of text hits before limiting", () => {
    const target = issue({ title: "no text overlap", number: 12, identifier: "V2-12", status: "cancelled" });
    const textHit = issue({ title: "notes about V2-12" });
    const engine = engineWith([textHit, target]);

    expect(engine.searchIssues({ q: "V2-12", include_closed: true, limit: 1 }).map((h) => h.id)).toEqual([target.id]);
    expect(engine.searchIssues({ q: "V2-12", limit: 1 }).map((h) => h.id)).toEqual([textHit.id]);
  });

  it("demotes cancelled issues unless the query targets them directly", () => {
    const cancelled = issue({ title: "billing export", status: "cancelled" });
    const live = issue({ title: "old billing export job", status: "done" });
    const engine = engineWith([cancelled, live]);

    expect(engine.searchIssues({ q: "billing", include_closed: true }).map((h) => h.id)).toEqual([live.id, cancelled.id]);
    expect(engine.searchIssues({ q: "Billing Export", include_closed: true }).map((h) => h.id)).toEqual([
      cancelled.id,
      live.id,
    ]);
  });

  it("drops terminal statuses unless include_closed, keeping unknown custom keys", () => {
    const open = issue({ title: "closure probe open" });
    const done = issue({ title: "closure probe done", status: "done" });
    const customDone = issue({ title: "closure probe shipped", status: "shipped", status_category: "done" });
    const customClosed = issue({ title: "closure probe dropped", status: "dropped", status_category: "closed" });
    const unknownKey = issue({ title: "closure probe legacy", status: "legacy_key" });
    const engine = engineWith([open, done, customDone, customClosed, unknownKey]);

    expect(new Set(engine.searchIssues({ q: "closure probe" }).map((h) => h.id))).toEqual(
      new Set([open.id, unknownKey.id]),
    );
    expect(engine.searchIssues({ q: "closure probe", include_closed: true })).toHaveLength(5);
  });

  it("lets terms spread across title, description and different comments", () => {
    const spread = issue({ title: "north part", description: "south part" });
    const partial = issue({ title: "north only" });
    const engine = engineWith(
      [spread, partial],
      [comment(spread.id, "east part", "2026-01-01T00:00:00Z"), comment(partial.id, "east part", "2026-01-01T00:00:00Z")],
    );

    const hits = engine.searchIssues({ q: "north south east" });
    expect(hits.map((h) => h.id)).toEqual([spread.id]);
    // No single comment holds every term, so there is no comment snippet.
    expect(hits[0]!.snippetCommentId).toBeNull();
  });

  it("ranks all terms in one comment above terms split across comments", () => {
    const same = issue({ title: "same comment" });
    const split = issue({ title: "split comments" });
    const at = "2026-01-01T00:00:00Z";
    const sameComment = comment(same.id, "red and blue", at);
    const engine = engineWith(
      [split, same],
      [sameComment, comment(split.id, "red", at), comment(split.id, "blue", at)],
    );

    const hits = engine.searchIssues({ q: "blue red" });
    expect(hits.map((h) => h.id)).toEqual([same.id, split.id]);
    expect(hits[0]!.snippetCommentId).toBe(sameComment.id);
  });

  it("picks the latest matching comment for the snippet", () => {
    const target = issue({ title: "snippet source" });
    const older = comment(target.id, "needle older", "2026-01-01T00:00:00.000001Z");
    const newer = comment(target.id, "needle newer", "2026-01-01T00:00:00.000002Z");
    const unrelated = comment(target.id, "nothing here", "2026-01-02T00:00:00Z");
    const engine = engineWith([target], [newer, unrelated, older]);

    expect(engine.searchIssues({ q: "needle" })[0]!.snippetCommentId).toBe(newer.id);
  });

  it("treats LIKE wildcards in the query as literal text", () => {
    const percent = issue({ title: "load 100% done" });
    const underscore = issue({ title: "a_b" });
    const decoy = issue({ title: "load 1000 done axb" });
    const engine = engineWith([percent, underscore, decoy]);

    expect(engine.searchIssues({ q: "100%" }).map((h) => h.id)).toEqual([percent.id]);
    // The server compares exact titles against the escaped phrase, so a title
    // with "_" never reaches the exact tier; it still starts with the phrase.
    const underscoreHits = engine.searchIssues({ q: "a_b" });
    expect(underscoreHits.map((h) => h.id)).toEqual([underscore.id]);
  });

  it("matches case-insensitively and splits on full-width spaces", () => {
    const target = issue({ title: "搜索 性能 Optimization" });
    const engine = engineWith([target]);

    expect(splitSearchTerms("搜索　optimization")).toEqual(["搜索", "optimization"]);
    expect(engine.searchIssues({ q: "OPTIMIZATION" }).map((h) => h.id)).toEqual([target.id]);
    expect(engine.searchIssues({ q: "optimization　搜索" }).map((h) => h.id)).toEqual([target.id]);
  });

  it("applies limit and offset after ranking, capped at 50", () => {
    const issues = Array.from({ length: 60 }, (_, i) =>
      issue({ title: `paged ${i}`, search_updated_at: `2026-01-01T00:00:${String(i).padStart(2, "0")}Z` }),
    );
    const engine = engineWith(issues);

    expect(engine.searchIssues({ q: "paged" })).toHaveLength(20);
    expect(engine.searchIssues({ q: "paged", limit: 500 })).toHaveLength(50);
    const second = engine.searchIssues({ q: "paged", limit: 5, offset: 5 });
    expect(second.map((h) => h.id)).toEqual(issues.slice(50, 55).reverse().map((i) => i.id));
  });

  it("forgets deleted issues and comments", () => {
    const target = issue({ title: "keeper" });
    const doomed = comment(target.id, "vanishing phrase", "2026-01-01T00:00:00Z");
    const engine = engineWith([target], [doomed]);
    expect(engine.searchIssues({ q: "vanishing" })).toHaveLength(1);

    engine.deleteComment(doomed.id);
    expect(engine.searchIssues({ q: "vanishing" })).toHaveLength(0);
    engine.upsertComment(comment(target.id, "orphan text", "2026-01-01T00:00:00Z"));
    engine.deleteIssues([target.id]);
    expect(engine.searchIssues({ q: "keeper" })).toHaveLength(0);
    expect(engine.size.comments).toBe(0);
  });
});

describe("toSearchIssueResult", () => {
  it("returns the server row shape with comment and description snippets", () => {
    const record = issue({ title: "Snippet row", description: "The description mentions a Needle once." });
    const matched = comment(record.id, "A comment with the needle inside", "2026-01-01T00:00:00Z");
    const row = toSearchIssueResult(record, { id: record.id, matchSource: "comment", snippetCommentId: matched.id }, "needle", matched);

    expect(row).not.toHaveProperty("search_updated_at");
    expect(row.match_source).toBe("comment");
    expect(row.matched_comment_snippet).toBe("A comment with the needle inside");
    expect(row.matched_snippet).toBe(row.matched_comment_snippet);
    expect(row.matched_description_snippet).toBe("The description mentions a Needle once.");
  });

  it("omits snippets that do not apply", () => {
    const record = issue({ title: "needle in title", description: "nothing" });
    const row = toSearchIssueResult(record, { id: record.id, matchSource: "title", snippetCommentId: null }, "needle", null);
    expect(row.matched_snippet).toBeUndefined();
    expect(row.matched_comment_snippet).toBeUndefined();
    expect(row.matched_description_snippet).toBeUndefined();
  });
});

describe("extractSnippet", () => {
  it("centres on the match and marks cut ends", () => {
    const content = `${"a".repeat(100)}needle${"b".repeat(100)}`;
    expect(extractSnippet(content, "NEEDLE")).toBe(`...${"a".repeat(40)}needle${"b".repeat(80)}...`);
  });

  it("counts code points, not UTF-16 units", () => {
    const content = `${"字".repeat(50)}搜索${"😀".repeat(100)}`;
    expect(extractSnippet(content, "搜索")).toBe(`...${"字".repeat(40)}搜索${"😀".repeat(80)}...`);
  });

  it("falls back to the earliest word, then to the leading text", () => {
    expect(extractSnippet("first beta then alpha", "alpha beta")).toBe("first beta then alpha");
    const long = "x".repeat(130);
    expect(extractSnippet(long, "missing")).toBe(`${"x".repeat(120)}...`);
    expect(extractSnippet("short", "missing")).toBe("short");
  });
});

describe("SearchIndexEngine.searchProjects", () => {
  it("ranks title tiers above description and demotes cancelled projects", () => {
    const exact = project({ title: "Roadmap" });
    const starts = project({ title: "Roadmap 2027" });
    const contains = project({ title: "The roadmap refresh" });
    const described = project({ title: "Planning", description: "covers the roadmap" });
    const cancelled = project({ title: "Old roadmap", status: "cancelled" });
    const completed = project({ title: "Roadmap done", status: "completed" });
    const engine = new SearchIndexEngine();
    [cancelled, described, contains, starts, exact, completed].forEach((p) => engine.upsertProject(p));

    expect(engine.searchProjects({ q: "roadmap" }).map((h) => h.id)).toEqual([
      exact.id,
      starts.id,
      contains.id,
      described.id,
    ]);
    const all = engine.searchProjects({ q: "roadmap", include_closed: true });
    expect(all.map((h) => h.id).at(-1)).toBe(cancelled.id);
    expect(all.find((h) => h.id === described.id)?.matchSource).toBe("description");

    const row = toSearchProjectResult(described, { id: described.id, matchSource: "description" }, "roadmap");
    expect(row.matched_snippet).toBe("covers the roadmap");
    expect(row).not.toHaveProperty("search_updated_at");
  });
});

describe("timestampMicros", () => {
  it("keeps sub-millisecond order", () => {
    expect(timestampMicros("2026-01-01T00:00:00.000002Z")).toBeGreaterThan(timestampMicros("2026-01-01T00:00:00.000001Z"));
    expect(timestampMicros("2026-01-01T00:00:00.1Z")).toBe(timestampMicros("2026-01-01T00:00:00.100000Z"));
    expect(timestampMicros("2026-01-01T08:00:00+08:00")).toBe(timestampMicros("2026-01-01T00:00:00Z"));
  });
});

describe("text budget accounting", () => {
  it("counts UTF-8 bytes the way the server's text_bytes does", () => {
    expect(utf8Length("abc")).toBe(3);
    expect(utf8Length("é")).toBe(2);
    expect(utf8Length("搜索")).toBe(6);
    expect(utf8Length("😀")).toBe(4);
  });

  it("follows upserts, updates, and deletes", () => {
    const engine = new SearchIndexEngine();
    const record = issue({ title: "abc", description: "搜索" });
    engine.upsertIssue(record);
    engine.upsertComment(comment(record.id, "hello", "2026-01-01T00:00:00Z", "c-1"));
    engine.upsertProject(project({ title: "p", description: "é" }));
    expect(engine.textBytes).toBe(3 + 6 + 5 + 1 + 2);

    engine.upsertIssue({ ...record, title: "abcd", description: null });
    expect(engine.textBytes).toBe(4 + 5 + 1 + 2);

    engine.deleteIssues([record.id]);
    expect(engine.textBytes).toBe(1 + 2);
    engine.clear();
    expect(engine.textBytes).toBe(0);
  });
});
