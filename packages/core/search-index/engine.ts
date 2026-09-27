// Import only dependency-free modules: this file runs inside the search index
// worker, and the issues/search barrels pull in React and the API client.
import { BUILT_IN_STATUS_CATEGORY, normalizeIssueStatusCategory } from "../issues/config/status";
import type {
  SearchIndexComment,
  SearchIndexIssue,
  SearchIndexProject,
  SearchIssueResult,
  SearchProjectResult,
} from "../types";

/**
 * In-memory search over one workspace's local index (MUL-7754).
 *
 * Matching and ranking mirror the server's `/api/issues/search` and
 * `/api/projects/search` (buildSearchQuery / buildProjectSearchQuery in
 * server/internal/handler) so a local result list is the one the server would
 * return. Only lowercased text lives here; full records are read back from the
 * store for the page being returned, which keeps memory close to one copy of
 * the text.
 */

export const DEFAULT_SEARCH_LIMIT = 20;
export const MAX_SEARCH_LIMIT = 50;

export interface IssueSearchParams {
  q: string;
  limit?: number;
  offset?: number;
  include_closed?: boolean;
}

export type ProjectSearchParams = IssueSearchParams;

export interface IssueHit {
  id: string;
  matchSource: SearchIssueResult["match_source"];
  /** Latest comment containing the phrase (or every term), for the snippet. */
  snippetCommentId: string | null;
}

export interface ProjectHit {
  id: string;
  matchSource: SearchProjectResult["match_source"];
}

interface IssueEntry {
  id: string;
  number: number;
  status: string;
  terminal: boolean;
  title: string;
  description: string;
  updatedAt: number;
  bytes: number;
}

interface CommentEntry {
  id: string;
  issueId: string;
  content: string;
  createdAt: number;
  bytes: number;
}

interface ProjectEntry {
  id: string;
  status: string;
  title: string;
  description: string;
  updatedAt: number;
  bytes: number;
}

/** UTF-8 length without encoding, the measure the server's text_bytes uses. */
export function utf8Length(s: string): number {
  let bytes = 0;
  for (let i = 0; i < s.length; i++) {
    const code = s.charCodeAt(i);
    if (code < 0x80) bytes += 1;
    else if (code < 0x800) bytes += 2;
    else if (code >= 0xd800 && code <= 0xdbff && i + 1 < s.length) {
      // A surrogate pair is one 4-byte code point.
      bytes += 4;
      i++;
    } else bytes += 3;
  }
  return bytes;
}

// Go's unicode.IsSpace, which the server splits terms on.
const SEARCH_SPACE = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/;

export function splitSearchTerms(q: string): string[] {
  return q.split(SEARCH_SPACE).filter((term) => term !== "");
}

/**
 * The server compares exact titles against the LIKE-escaped phrase, so a title
 * containing `_`, `%` or `\` never counts as an exact match. Kept on purpose.
 */
function escapeLike(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/%/g, "\\%").replace(/_/g, "\\_");
}

/**
 * Microseconds since the epoch from an RFC 3339 timestamp, keeping the
 * sub-millisecond digits the server orders by (Date only keeps milliseconds).
 */
export function timestampMicros(value: string): number {
  const match = /^(.*?)(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) {
    const ms = Date.parse(value);
    return Number.isNaN(ms) ? 0 : ms * 1000;
  }
  const seconds = Date.parse(`${match[1]}${match[3]}`);
  if (Number.isNaN(seconds)) return 0;
  const fraction = (match[2] ?? "").slice(0, 6).padEnd(6, "0");
  return seconds * 1000 + Number(fraction);
}

const STATUS_RANK: Record<string, number> = {
  in_progress: 0,
  in_review: 1,
  todo: 2,
  blocked: 3,
  backlog: 4,
  done: 5,
  cancelled: 6,
};

function isTerminal(issue: Pick<SearchIndexIssue, "status" | "status_category">): boolean {
  // Unknown legacy keys stay searchable, matching the server, which only
  // excludes keys it resolves to a terminal category.
  const category =
    (issue.status_category ? normalizeIssueStatusCategory(issue.status_category) : null) ??
    (Object.hasOwn(BUILT_IN_STATUS_CATEGORY, issue.status)
      ? BUILT_IN_STATUS_CATEGORY[issue.status as keyof typeof BUILT_IN_STATUS_CATEGORY]
      : null);
  return category === "done" || category === "closed";
}

/** Server parseQueryNumber: "MUL-123" or a bare "123". */
function parseQueryNumber(query: string): number | null {
  const q = query.trim();
  const match = /^[a-z]+-(\d+)$/i.exec(q) ?? /^(\d+)$/.exec(q);
  if (!match) return null;
  const n = Number.parseInt(match[1]!, 10);
  return Number.isInteger(n) && n > 0 ? n : null;
}

export class SearchIndexEngine {
  private readonly issues = new Map<string, IssueEntry>();
  private readonly comments = new Map<string, CommentEntry>();
  private readonly projects = new Map<string, ProjectEntry>();

  private bytes = 0;

  get size(): { issues: number; comments: number; projects: number } {
    return { issues: this.issues.size, comments: this.comments.size, projects: this.projects.size };
  }

  /**
   * UTF-8 bytes of every title, description, and comment held, counted the
   * way the manifest's text_bytes is, so the memory budget can be enforced as
   * the workspace grows and not only when the copy is first built.
   */
  get textBytes(): number {
    return this.bytes;
  }

  clear(): void {
    this.issues.clear();
    this.comments.clear();
    this.projects.clear();
    this.bytes = 0;
  }

  upsertIssue(issue: SearchIndexIssue): void {
    const description = issue.description ?? "";
    const entry: IssueEntry = {
      id: issue.id,
      number: issue.number,
      status: issue.status,
      terminal: isTerminal(issue),
      title: issue.title.toLowerCase(),
      description: description.toLowerCase(),
      updatedAt: timestampMicros(issue.search_updated_at || issue.updated_at),
      bytes: utf8Length(issue.title) + utf8Length(description),
    };
    this.bytes += entry.bytes - (this.issues.get(issue.id)?.bytes ?? 0);
    this.issues.set(issue.id, entry);
  }

  upsertComment(comment: SearchIndexComment): void {
    const entry: CommentEntry = {
      id: comment.id,
      issueId: comment.issue_id,
      content: comment.content.toLowerCase(),
      createdAt: timestampMicros(comment.created_at),
      bytes: utf8Length(comment.content),
    };
    this.bytes += entry.bytes - (this.comments.get(comment.id)?.bytes ?? 0);
    this.comments.set(comment.id, entry);
  }

  upsertProject(project: SearchIndexProject): void {
    const description = project.description ?? "";
    const entry: ProjectEntry = {
      id: project.id,
      status: project.status,
      title: project.title.toLowerCase(),
      description: description.toLowerCase(),
      updatedAt: timestampMicros(project.search_updated_at || project.updated_at),
      bytes: utf8Length(project.title) + utf8Length(description),
    };
    this.bytes += entry.bytes - (this.projects.get(project.id)?.bytes ?? 0);
    this.projects.set(project.id, entry);
  }

  /** Removes the issues and every comment on them. */
  deleteIssues(ids: readonly string[]): void {
    if (ids.length === 0) return;
    const doomed = new Set(ids);
    for (const id of doomed) {
      this.bytes -= this.issues.get(id)?.bytes ?? 0;
      this.issues.delete(id);
    }
    for (const [id, comment] of this.comments) {
      if (doomed.has(comment.issueId)) {
        this.bytes -= comment.bytes;
        this.comments.delete(id);
      }
    }
  }

  deleteComment(id: string): void {
    this.bytes -= this.comments.get(id)?.bytes ?? 0;
    this.comments.delete(id);
  }

  deleteProject(id: string): void {
    this.bytes -= this.projects.get(id)?.bytes ?? 0;
    this.projects.delete(id);
  }

  searchIssues(params: IssueSearchParams): IssueHit[] {
    const phrase = params.q.toLowerCase();
    if (phrase === "") return [];
    const terms = splitSearchTerms(phrase);
    const multi = terms.length > 1;
    const number = parseQueryNumber(params.q);
    const escapedPhrase = escapeLike(phrase);

    // One pass over the comments, keeping per-issue flags and the latest
    // comment that can supply a snippet.
    interface CommentAgg {
      phrase: boolean;
      terms: boolean[];
      allTerms: boolean;
      snippet: CommentEntry | null;
    }
    const byIssue = new Map<string, CommentAgg>();
    // Reused per comment: this loop runs over every comment in the workspace.
    const termHits = terms.map(() => false);
    for (const comment of this.comments.values()) {
      const content = comment.content;
      const hasPhrase = content.includes(phrase);
      let anyTerm = false;
      let allTerms = multi;
      if (multi) {
        for (let i = 0; i < terms.length; i++) {
          const hit = content.includes(terms[i]!);
          termHits[i] = hit;
          if (hit) anyTerm = true;
          else allTerms = false;
        }
      }
      if (!hasPhrase && !anyTerm) continue;
      let agg = byIssue.get(comment.issueId);
      if (!agg) {
        agg = { phrase: false, terms: terms.map(() => false), allTerms: false, snippet: null };
        byIssue.set(comment.issueId, agg);
      }
      agg.phrase ||= hasPhrase;
      if (multi) {
        for (let i = 0; i < terms.length; i++) {
          if (termHits[i]) agg.terms[i] = true;
        }
      }
      agg.allTerms ||= allTerms;
      if (hasPhrase || allTerms) {
        const current = agg.snippet;
        if (
          !current ||
          comment.createdAt > current.createdAt ||
          (comment.createdAt === current.createdAt && comment.id > current.id)
        ) {
          agg.snippet = comment;
        }
      }
    }

    const { offset, limit } = page(params);
    // Only the requested page is needed, so keep the best offset+limit
    // candidates instead of sorting every match (tens of thousands for a
    // common word in a large workspace).
    const top = new TopK<IssueCandidate>(offset + limit, compareIssueCandidates);
    const titleTerms = terms.map(() => false);
    const descriptionTerms = terms.map(() => false);
    for (const issue of this.issues.values()) {
      if (!params.include_closed && issue.terminal) continue;
      const title = issue.title;
      const titleExact = title === escapedPhrase;
      const titleStartsWith = title.startsWith(phrase);
      const titlePhrase = title.includes(phrase);
      let titleAllTerms = multi;
      if (multi) {
        for (let i = 0; i < terms.length; i++) {
          titleTerms[i] = title.includes(terms[i]!);
          if (!titleTerms[i]) titleAllTerms = false;
        }
      }
      // Once the title matched, it outranks anything the description could
      // add, so the server never evaluates the description; neither do we.
      const titleMatched = titlePhrase || titleAllTerms;
      const description = issue.description;
      const descriptionPhrase = !titleMatched && description.includes(phrase);
      let descriptionAllTerms = multi;
      if (multi) {
        for (let i = 0; i < terms.length; i++) {
          descriptionTerms[i] = !titleMatched && description.includes(terms[i]!);
          if (!descriptionTerms[i]) descriptionAllTerms = false;
        }
      }
      const numberExact = number !== null && issue.number === number;
      const comments = byIssue.get(issue.id);
      const commentPhrase = comments?.phrase ?? false;

      let eligible = titlePhrase || descriptionPhrase || commentPhrase || numberExact;
      if (!eligible && multi) {
        eligible = true;
        for (let i = 0; i < terms.length; i++) {
          if (!(titleTerms[i] || descriptionTerms[i] || (comments?.terms[i] ?? false))) {
            eligible = false;
            break;
          }
        }
      }
      if (!eligible) continue;

      let relevance = 9;
      if (numberExact) relevance = 0;
      else if (titleExact) relevance = 1;
      else if (titleStartsWith) relevance = 2;
      else if (titlePhrase) relevance = 3;
      else if (titleAllTerms) relevance = 4;
      else if (descriptionPhrase) relevance = 5;
      else if (descriptionAllTerms) relevance = 6;
      else if (commentPhrase) relevance = 7;
      else if (multi && comments?.allTerms) relevance = 8;

      const candidate: IssueCandidate = {
        id: issue.id,
        cancelledRank: issue.status === "cancelled" && !(titleExact || numberExact) ? 1 : 0,
        relevance,
        statusRank: STATUS_RANK[issue.status] ?? 7,
        updatedAt: issue.updatedAt,
        matchSource: titleMatched ? "title" : descriptionPhrase || descriptionAllTerms ? "description" : "comment",
        snippetCommentId: comments?.snippet?.id ?? null,
      };
      top.offer(candidate);
    }

    return top
      .sorted()
      .slice(offset)
      .map((c) => ({ id: c.id, matchSource: c.matchSource, snippetCommentId: c.snippetCommentId }));
  }

  searchProjects(params: ProjectSearchParams): ProjectHit[] {
    const phrase = params.q.toLowerCase();
    if (phrase === "") return [];
    const terms = splitSearchTerms(phrase);
    const multi = terms.length > 1;
    const escapedPhrase = escapeLike(phrase);

    interface Candidate {
      hit: ProjectHit;
      cancelledRank: number;
      relevance: number;
      updatedAt: number;
    }
    const candidates: Candidate[] = [];
    for (const project of this.projects.values()) {
      if (!params.include_closed && (project.status === "completed" || project.status === "cancelled")) continue;
      const { title, description } = project;
      const titlePhrase = title.includes(phrase);
      const titleAllTerms = multi && terms.every((term) => title.includes(term));
      const descriptionPhrase = description.includes(phrase);
      const eligible =
        titlePhrase ||
        descriptionPhrase ||
        (multi && terms.every((term) => title.includes(term) || description.includes(term)));
      if (!eligible) continue;

      const titleExact = title === escapedPhrase;
      let relevance = 5;
      if (titleExact) relevance = 0;
      else if (title.startsWith(phrase)) relevance = 1;
      else if (titlePhrase) relevance = 2;
      else if (titleAllTerms) relevance = 3;
      else if (descriptionPhrase) relevance = 4;

      candidates.push({
        hit: { id: project.id, matchSource: titlePhrase || titleAllTerms ? "title" : "description" },
        cancelledRank: project.status === "cancelled" && !titleExact ? 1 : 0,
        relevance,
        updatedAt: project.updatedAt,
      });
    }

    // The server has no final tie-breaker here; the id keeps local order stable.
    candidates.sort(
      (a, b) =>
        a.cancelledRank - b.cancelledRank ||
        a.relevance - b.relevance ||
        b.updatedAt - a.updatedAt ||
        (a.hit.id < b.hit.id ? -1 : a.hit.id > b.hit.id ? 1 : 0),
    );
    const { offset, limit } = page(params);
    return candidates.slice(offset, offset + limit).map((c) => c.hit);
  }
}

interface IssueCandidate extends IssueHit {
  cancelledRank: number;
  relevance: number;
  statusRank: number;
  updatedAt: number;
}

function compareIssueCandidates(a: IssueCandidate, b: IssueCandidate): number {
  return (
    a.cancelledRank - b.cancelledRank ||
    a.relevance - b.relevance ||
    a.statusRank - b.statusRank ||
    b.updatedAt - a.updatedAt ||
    (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  );
}

/** Keeps the `capacity` smallest items under `compare`, in order. */
class TopK<T> {
  private readonly items: T[] = [];

  constructor(
    private readonly capacity: number,
    private readonly compare: (a: T, b: T) => number,
  ) {}

  offer(item: T): void {
    const items = this.items;
    if (items.length === this.capacity && this.compare(item, items[items.length - 1]!) >= 0) return;
    let lo = 0;
    let hi = items.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (this.compare(items[mid]!, item) <= 0) lo = mid + 1;
      else hi = mid;
    }
    items.splice(lo, 0, item);
    if (items.length > this.capacity) items.pop();
  }

  sorted(): T[] {
    return this.items;
  }
}

function page(params: IssueSearchParams): { offset: number; limit: number } {
  const limit =
    params.limit !== undefined && params.limit > 0 ? Math.min(params.limit, MAX_SEARCH_LIMIT) : DEFAULT_SEARCH_LIMIT;
  const offset = params.offset !== undefined && params.offset > 0 ? params.offset : 0;
  return { offset, limit };
}

/**
 * Port of the server's extractSnippet: up to ~120 code points around the
 * first case-insensitive occurrence of the query (or, for several words, of the
 * earliest word).
 */
export function extractSnippet(content: string, query: string): string {
  const runes = Array.from(content);
  const lowerRunes = Array.from(content.toLowerCase());
  const queryRunes = Array.from(query.toLowerCase());

  let idx = findRunes(lowerRunes, queryRunes);
  let matchLen = queryRunes.length;
  if (idx < 0) {
    const terms = splitSearchTerms(query.toLowerCase());
    if (terms.length > 1) {
      let earliest = -1;
      let earliestLen = 0;
      for (const term of terms) {
        const termRunes = Array.from(term);
        const pos = findRunes(lowerRunes, termRunes);
        if (pos >= 0 && (earliest < 0 || pos < earliest)) {
          earliest = pos;
          earliestLen = termRunes.length;
        }
      }
      if (earliest >= 0) {
        idx = earliest;
        matchLen = earliestLen;
      }
    }
  }

  if (idx < 0) {
    return runes.length > 120 ? `${runes.slice(0, 120).join("")}...` : content;
  }
  const start = Math.max(idx - 40, 0);
  const end = Math.min(idx + matchLen + 80, runes.length);
  let snippet = runes.slice(start, end).join("");
  if (start > 0) snippet = `...${snippet}`;
  if (end < runes.length) snippet = `${snippet}...`;
  return snippet;
}

function findRunes(haystack: string[], needle: string[]): number {
  if (needle.length === 0 || haystack.length < needle.length) return -1;
  outer: for (let i = 0; i <= haystack.length - needle.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return i;
  }
  return -1;
}

/** Server `descriptionContains`: the phrase, or (for several words) every word. */
function descriptionContains(description: string, query: string, terms: string[]): boolean {
  if (description === "") return false;
  const lower = description.toLowerCase();
  if (lower.includes(query.toLowerCase())) return true;
  return terms.length > 1 && terms.every((term) => lower.includes(term.toLowerCase()));
}

/** Builds the response row the server would return for one hit. */
export function toSearchIssueResult(
  issue: SearchIndexIssue,
  hit: IssueHit,
  q: string,
  snippetComment: SearchIndexComment | null,
): SearchIssueResult {
  // The index keeps a precise timestamp for ranking; the result carries the
  // same fields as a server search row.
  const { search_updated_at: _searchUpdatedAt, ...rest } = issue;
  const result: SearchIssueResult = { ...rest, match_source: hit.matchSource };
  if (snippetComment && snippetComment.content !== "") {
    const snippet = extractSnippet(snippetComment.content, q);
    result.matched_comment_snippet = snippet;
    if (hit.matchSource === "comment") result.matched_snippet = snippet;
  }
  const description = issue.description ?? "";
  if (
    (hit.matchSource === "description" || descriptionContains(description, q, splitSearchTerms(q))) &&
    description !== ""
  ) {
    result.matched_description_snippet = extractSnippet(description, q);
  }
  return result;
}

export function toSearchProjectResult(
  project: SearchIndexProject,
  hit: ProjectHit,
  q: string,
): SearchProjectResult {
  const { search_updated_at: _searchUpdatedAt, ...rest } = project;
  const result: SearchProjectResult = { ...rest, match_source: hit.matchSource };
  const description = project.description ?? "";
  if (hit.matchSource === "description" && description !== "") {
    result.matched_snippet = extractSnippet(description, q);
  }
  return result;
}
